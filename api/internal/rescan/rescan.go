// Package rescan re-checks recently created links against Safe Browsing and
// takes down the ones Google has listed since they were created (D27). It
// closes the gap the create-time check (D23) cannot see: a URL that was
// clean, or unknown, when it was shortened.
package rescan

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/url"
	"strings"
	"time"

	"github.com/azizhudaya/shortlink/api/internal/alert"
	"github.com/azizhudaya/shortlink/api/internal/safety"
	"github.com/azizhudaya/shortlink/api/internal/store"
)

type Store interface {
	RescanCandidates(ctx context.Context, since time.Time, afterCode string, limit int) ([]store.RescanLink, error)
	SystemTakedown(ctx context.Context, code, reason string) (changed bool, ownerFlagged int, err error)
	BanUserByID(ctx context.Context, userID int64, disabledBy, reason string) (links, sessions int, err error)
}

type Rescanner struct {
	Window          time.Duration // how far back links are rechecked
	Page            int           // links per Safe Browsing request
	NewAccountDays  int           // owners younger than this are banned on a first hit
	RepeatThreshold int           // flagged links that get an older owner banned

	st      Store
	checker safety.BatchChecker
	notify  alert.Notifier
	logger  *slog.Logger
	now     func() time.Time
}

func New(st Store, checker safety.BatchChecker, notify alert.Notifier, logger *slog.Logger) *Rescanner {
	return &Rescanner{
		Window:          30 * 24 * time.Hour,
		Page:            safety.MaxBatch,
		NewAccountDays:  7,
		RepeatThreshold: 2,
		st:              st,
		checker:         checker,
		notify:          notify,
		logger:          logger,
		now:             time.Now,
	}
}

type Report struct {
	Scanned, TakenDown, Banned int
	Actions                    []Action
}

type Action struct {
	Code, Host string
	UserID     *int64
	Action     string // takedown | takedown+ban | takedown, review owner
}

const (
	actionTakedown = "takedown"
	actionBan      = "takedown+ban"
	actionReview   = "takedown, review owner"
)

// RunOnce makes one pass. If Safe Browsing fails, the pass stops there:
// takedowns already made stand, and the next pass retries the rest.
func (r *Rescanner) RunOnce(ctx context.Context) (rep Report, err error) {
	defer func() { r.sendAlerts(ctx, rep.Actions) }()

	since := r.now().Add(-r.Window)
	banned := map[int64]bool{}
	after := ""
	for {
		batch, err := r.st.RescanCandidates(ctx, since, after, r.Page)
		if err != nil {
			return rep, err
		}
		if len(batch) == 0 {
			return rep, nil
		}
		flagged, err := r.checker.CheckBatch(ctx, uniqueURLs(batch))
		if err != nil {
			return rep, err
		}
		for _, l := range batch {
			rep.Scanned++
			threat, ok := flagged[l.LongURL]
			if !ok {
				continue
			}
			if err := r.takeDown(ctx, l, threat, banned, &rep); err != nil {
				return rep, err
			}
		}
		if len(batch) < r.Page {
			return rep, nil
		}
		after = batch[len(batch)-1].Code
	}
}

func (r *Rescanner) takeDown(ctx context.Context, l store.RescanLink, threat string, banned map[int64]bool, rep *Report) error {
	changed, ownerFlagged, err := r.st.SystemTakedown(ctx, l.Code, store.FlaggedReasonPrefix+" "+threat)
	if err != nil {
		return fmt.Errorf("taking down %s: %w", l.Code, err)
	}
	if !changed {
		return nil // already down, e.g. by its owner's ban earlier in this pass
	}
	rep.TakenDown++

	act := Action{Code: l.Code, Host: hostOf(l.LongURL), UserID: l.UserID, Action: actionTakedown}
	if id := l.UserID; id != nil && l.UserActive && !banned[*id] {
		newAccount := r.now().Sub(l.UserCreatedAt) < time.Duration(r.NewAccountDays)*24*time.Hour
		if !l.UserWhitelisted && (newAccount || ownerFlagged >= r.RepeatThreshold) {
			if _, _, err := r.st.BanUserByID(ctx, *id, store.SystemSafeBrowsing,
				"banned after safe browsing match on "+l.Code); err != nil {
				return fmt.Errorf("banning user %d: %w", *id, err)
			}
			banned[*id] = true
			rep.Banned++
			act.Action = actionBan
		} else {
			act.Action = actionReview
		}
	}
	rep.Actions = append(rep.Actions, act)
	r.logger.Error("rescan_takedown", "code", act.Code, "host", act.Host, "user_id", act.UserID, "action", act.Action)
	return nil
}

// sendAlerts sends one message per owner per pass, carrying the most severe
// action, and one per ownerless link. A failed alert is logged and ignored:
// the takedowns have already happened.
func (r *Rescanner) sendAlerts(ctx context.Context, actions []Action) {
	type group struct {
		action       string
		codes, hosts []string
		user         string
	}
	var order []*group
	byOwner := map[int64]*group{}
	for _, a := range actions {
		var g *group
		if a.UserID != nil {
			g = byOwner[*a.UserID]
		}
		if g == nil {
			g = &group{action: a.Action, user: "none"}
			order = append(order, g)
			if a.UserID != nil {
				byOwner[*a.UserID] = g
				g.user = fmt.Sprint(*a.UserID)
			}
		}
		if a.Action == actionBan {
			g.action = actionBan
		}
		g.codes = append(g.codes, a.Code)
		g.hosts = append(g.hosts, a.Host)
	}
	for _, g := range order {
		msg := fmt.Sprintf("[shortlink] %s code=%s host=%s user=%s",
			g.action, strings.Join(g.codes, ","), strings.Join(g.hosts, ","), g.user)
		if err := r.notify.Notify(ctx, msg); err != nil {
			r.logger.Warn("rescan alert failed", "error", err)
		}
	}
}

// Loop runs a pass after `first`, then every `every` ± up to `jitter`, until
// ctx is cancelled.
func (r *Rescanner) Loop(ctx context.Context, first, every, jitter time.Duration) {
	wait := first
	for {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}

		rep, err := r.RunOnce(ctx)
		switch {
		case ctx.Err() != nil:
			return
		case errors.Is(err, safety.ErrUnavailable):
			r.logger.Warn("rescan stopped: safe browsing unavailable", "error", err, "scanned", rep.Scanned)
		case err != nil:
			r.logger.Error("rescan failed", "error", err, "scanned", rep.Scanned)
		default:
			r.logger.Info("rescan", "scanned", rep.Scanned, "taken_down", rep.TakenDown, "banned", rep.Banned)
		}
		wait = every
		if jitter > 0 {
			wait += time.Duration(rand.Int64N(int64(2*jitter)+1)) - jitter
		}
	}
}

func uniqueURLs(batch []store.RescanLink) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range batch {
		if !seen[l.LongURL] {
			seen[l.LongURL] = true
			out = append(out, l.LongURL)
		}
	}
	return out
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
