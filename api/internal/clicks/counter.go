// Package clicks counts redirects served (D32, FR-26). The redirect handler
// only increments a map in memory; Run writes the totals to the database in
// one batch a minute and once more on shutdown. A crash loses at most one
// interval of counts, which is accepted: the redirect must stay a single read.
package clicks

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/azizhudaya/shortlink/api/internal/store"
)

type Store interface {
	AddClicks(ctx context.Context, counts []store.ClickCount) error
}

type key struct {
	code, day string
}

type Counter struct {
	st      Store
	logger  *slog.Logger
	now     func() time.Time
	maxKeys int // distinct (code, day) pairs held between flushes

	mu      sync.Mutex
	pending map[key]int64
	dropped int64
}

func New(st Store, logger *slog.Logger) *Counter {
	return &Counter{
		st:      st,
		logger:  logger,
		now:     time.Now,
		maxKeys: 100_000,
		pending: map[key]int64{},
	}
}

// Record counts one redirect. It never blocks on I/O and never fails: past
// maxKeys, clicks on codes not already pending are dropped and logged at the
// next flush.
func (c *Counter) Record(code string) {
	k := key{code: code, day: c.now().UTC().Format(time.DateOnly)}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.pending[k]; !ok && len(c.pending) >= c.maxKeys {
		c.dropped++
		return
	}
	c.pending[k]++
}

// Flush writes everything pending. If the write fails, the counts go back
// into memory for the next attempt.
func (c *Counter) Flush(ctx context.Context) error {
	c.mu.Lock()
	batch, dropped := c.pending, c.dropped
	c.pending, c.dropped = map[key]int64{}, 0
	c.mu.Unlock()

	if dropped > 0 {
		c.logger.Warn("click counter full; clicks dropped", "dropped", dropped)
	}
	if len(batch) == 0 {
		return nil
	}
	counts := make([]store.ClickCount, 0, len(batch))
	for k, n := range batch {
		counts = append(counts, store.ClickCount{Code: k.code, Day: k.day, Clicks: n})
	}
	if err := c.st.AddClicks(ctx, counts); err != nil {
		c.mu.Lock()
		for k, n := range batch {
			if _, ok := c.pending[k]; ok || len(c.pending) < c.maxKeys {
				c.pending[k] += n
			}
		}
		c.mu.Unlock()
		return err
	}
	return nil
}

// Run flushes every interval until ctx is cancelled, then flushes once more
// with a short deadline of its own so a clean shutdown loses nothing.
func (c *Counter) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := c.Flush(final); err != nil {
				c.logger.Error("flushing click counts on shutdown", "error", err)
			}
			return
		case <-t.C:
			if err := c.Flush(ctx); err != nil && ctx.Err() == nil {
				c.logger.Error("flushing click counts", "error", err)
			}
		}
	}
}
