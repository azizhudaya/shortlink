package rescan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/azizhudaya/shortlink/api/internal/safety"
	"github.com/azizhudaya/shortlink/api/internal/store"
)

var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// fakeStore keeps links in memory with the same semantics as Postgres.
type fakeStore struct {
	links    map[string]*store.RescanLink
	disabled map[string]string // code → disabled_by
	flagged  map[int64]int     // owner → direct system takedowns
	banned   []int64
}

func newFakeStore(links ...store.RescanLink) *fakeStore {
	f := &fakeStore{links: map[string]*store.RescanLink{}, disabled: map[string]string{}, flagged: map[int64]int{}}
	for i := range links {
		l := links[i]
		f.links[l.Code] = &l
	}
	return f
}

func (f *fakeStore) RescanCandidates(_ context.Context, _ time.Time, after string, limit int) ([]store.RescanLink, error) {
	var codes []string
	for c := range f.links {
		if c > after && f.disabled[c] == "" {
			codes = append(codes, c)
		}
	}
	sort.Strings(codes)
	var out []store.RescanLink
	for _, c := range codes {
		if len(out) == limit {
			break
		}
		out = append(out, *f.links[c])
	}
	return out, nil
}

func (f *fakeStore) SystemTakedown(_ context.Context, code, _ string) (bool, int, error) {
	l := f.links[code]
	if f.disabled[code] != "" {
		if l.UserID == nil {
			return false, 0, nil
		}
		return false, f.flagged[*l.UserID], nil
	}
	f.disabled[code] = "system:safebrowsing"
	if l.UserID == nil {
		return true, 0, nil
	}
	f.flagged[*l.UserID]++
	return true, f.flagged[*l.UserID], nil
}

func (f *fakeStore) BanUserByID(_ context.Context, id int64, disabledBy, _ string) (int, int, error) {
	f.banned = append(f.banned, id)
	n := 0
	for c, l := range f.links {
		if l.UserID != nil && *l.UserID == id {
			l.UserActive = false
			if f.disabled[c] == "" {
				f.disabled[c] = disabledBy
				n++
			}
		}
	}
	return n, 0, nil
}

type fakeChecker struct {
	bad   map[string]string
	err   error
	calls int
}

func (c *fakeChecker) CheckBatch(_ context.Context, urls []string) (map[string]string, error) {
	c.calls++
	if len(urls) > 500 {
		return nil, errors.New("too many")
	}
	if c.err != nil {
		return nil, c.err
	}
	out := map[string]string{}
	for _, u := range urls {
		if t, ok := c.bad[u]; ok {
			out[u] = t
		}
	}
	return out, nil
}

type recorder struct {
	mu   sync.Mutex
	msgs []string
	err  error
}

func (r *recorder) Notify(_ context.Context, msg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, msg)
	return r.err
}

func owner(id int64) *int64 { return &id }

func link(code string, user *int64, age time.Duration) store.RescanLink {
	return store.RescanLink{Code: code, LongURL: "https://" + code + ".example/", UserID: user,
		UserCreatedAt: now.Add(-age), UserActive: user != nil}
}

func newTest(st *fakeStore, bad map[string]string) (*Rescanner, *fakeChecker, *recorder) {
	c := &fakeChecker{bad: bad}
	n := &recorder{}
	r := New(st, c, n, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.now = func() time.Time { return now }
	return r, c, n
}

const day = 24 * time.Hour

func TestNewAccountFlaggedIsBanned(t *testing.T) {
	st := newFakeStore(link("bad", owner(1), 2*day), link("fine", owner(1), 2*day), link("other", owner(2), 2*day))
	r, _, n := newTest(st, map[string]string{"https://bad.example/": "SOCIAL_ENGINEERING"})

	rep, err := r.RunOnce(context.Background())
	if err != nil || rep.Scanned != 3 || rep.TakenDown != 1 || rep.Banned != 1 {
		t.Fatalf("report %+v, err %v", rep, err)
	}
	if fmt.Sprint(st.banned) != "[1]" || st.disabled["fine"] != "system:safebrowsing" || st.disabled["other"] != "" {
		t.Fatalf("banned %v, disabled %v", st.banned, st.disabled)
	}
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "code=bad") || !strings.Contains(n.msgs[0], "host=bad.example") ||
		!strings.Contains(n.msgs[0], "user=1") || strings.Contains(n.msgs[0], "https://") {
		t.Fatalf("alerts %q", n.msgs)
	}
}

func TestMatureAccountSingleHitLinkOnly(t *testing.T) {
	st := newFakeStore(link("bad", owner(1), 30*day), link("fine", owner(1), 30*day))
	r, _, n := newTest(st, map[string]string{"https://bad.example/": "MALWARE"})

	rep, _ := r.RunOnce(context.Background())
	if rep.TakenDown != 1 || rep.Banned != 0 || len(st.banned) != 0 || st.disabled["fine"] != "" {
		t.Fatalf("report %+v, banned %v, disabled %v", rep, st.banned, st.disabled)
	}
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "review owner") {
		t.Fatalf("alerts %q", n.msgs)
	}
}

func TestMatureAccountSecondHitBanned(t *testing.T) {
	st := newFakeStore(link("bad1", owner(1), 30*day), link("bad2", owner(1), 30*day), link("fine", owner(1), 30*day))
	r, _, n := newTest(st, map[string]string{"https://bad1.example/": "MALWARE", "https://bad2.example/": "MALWARE"})

	rep, _ := r.RunOnce(context.Background())
	if rep.TakenDown != 2 || rep.Banned != 1 || fmt.Sprint(st.banned) != "[1]" {
		t.Fatalf("report %+v, banned %v", rep, st.banned)
	}
	if len(n.msgs) != 1 {
		t.Fatalf("owner notified %d times in one run: %q", len(n.msgs), n.msgs)
	}
}

func TestM1LinkTakenDownNoBan(t *testing.T) {
	st := newFakeStore(link("m1", nil, 0))
	r, _, n := newTest(st, map[string]string{"https://m1.example/": "MALWARE"})
	rep, _ := r.RunOnce(context.Background())
	if rep.TakenDown != 1 || rep.Banned != 0 || len(n.msgs) != 1 {
		t.Fatalf("report %+v, alerts %q", rep, n.msgs)
	}
}

func TestAlreadyInactiveOwnerNotRebanned(t *testing.T) {
	l := link("bad", owner(1), 2*day)
	l.UserActive = false
	st := newFakeStore(l)
	r, _, _ := newTest(st, map[string]string{"https://bad.example/": "MALWARE"})
	rep, _ := r.RunOnce(context.Background())
	if rep.TakenDown != 1 || rep.Banned != 0 || len(st.banned) != 0 {
		t.Fatalf("report %+v, banned %v", rep, st.banned)
	}
}

func TestWhitelistedNeverAutoBanned(t *testing.T) {
	a, b := link("bad1", owner(1), 2*day), link("bad2", owner(1), 2*day)
	a.UserWhitelisted, b.UserWhitelisted = true, true
	st := newFakeStore(a, b)
	r, _, n := newTest(st, map[string]string{"https://bad1.example/": "MALWARE", "https://bad2.example/": "MALWARE"})

	rep, _ := r.RunOnce(context.Background())
	if rep.TakenDown != 2 || rep.Banned != 0 || len(st.banned) != 0 {
		t.Fatalf("report %+v, banned %v", rep, st.banned)
	}
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "review owner") {
		t.Fatalf("alerts %q", n.msgs)
	}
}

func TestCheckerErrorStopsRunNoTakedowns(t *testing.T) {
	st := newFakeStore(link("bad", owner(1), 2*day))
	r, c, n := newTest(st, map[string]string{"https://bad.example/": "MALWARE"})
	c.err = fmt.Errorf("%w: timeout", safety.ErrUnavailable)

	rep, err := r.RunOnce(context.Background())
	if !errors.Is(err, safety.ErrUnavailable) || rep.TakenDown != 0 || len(st.disabled) != 0 || len(n.msgs) != 0 {
		t.Fatalf("report %+v, err %v, disabled %v", rep, err, st.disabled)
	}
}

func TestPagesOver500(t *testing.T) {
	var links []store.RescanLink
	for i := 0; i < 1201; i++ {
		links = append(links, link(fmt.Sprintf("c%04d", i), owner(int64(i+10)), 30*day))
	}
	st := newFakeStore(links...)
	r, c, _ := newTest(st, map[string]string{"https://c1200.example/": "MALWARE"})

	rep, err := r.RunOnce(context.Background())
	if err != nil || rep.Scanned != 1201 || rep.TakenDown != 1 || c.calls != 3 {
		t.Fatalf("report %+v, err %v, batches %d", rep, err, c.calls)
	}
}

func TestNotifierFailureDoesNotBlockTakedown(t *testing.T) {
	st := newFakeStore(link("bad", owner(1), 2*day))
	r, _, n := newTest(st, map[string]string{"https://bad.example/": "MALWARE"})
	n.err = errors.New("webhook down")
	rep, err := r.RunOnce(context.Background())
	if err != nil || rep.TakenDown != 1 || rep.Banned != 1 {
		t.Fatalf("report %+v, err %v", rep, err)
	}
}

func TestLoopStopsOnCancel(t *testing.T) {
	st := newFakeStore(link("ok", owner(1), 30*day))
	r, c, _ := newTest(st, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Loop(ctx, time.Millisecond, 5*time.Millisecond, time.Millisecond)
		close(done)
	}()
	time.Sleep(40 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Loop did not stop")
	}
	if c.calls < 2 {
		t.Fatalf("runs = %d, want several", c.calls)
	}
}
