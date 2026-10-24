package clicks

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/azizhudaya/shortlink/api/internal/store"
)

type fakeStore struct {
	mu      sync.Mutex
	batches [][]store.ClickCount
	fail    error
}

func (f *fakeStore) AddClicks(_ context.Context, counts []store.ClickCount) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		err := f.fail
		f.fail = nil
		return err
	}
	cp := append([]store.ClickCount(nil), counts...)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].Day != cp[j].Day {
			return cp[i].Day < cp[j].Day
		}
		return cp[i].Code < cp[j].Code
	})
	f.batches = append(f.batches, cp)
	return nil
}

func newTest(st *fakeStore) (*Counter, *time.Time) {
	c := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Date(2026, 10, 24, 23, 59, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	return c, &now
}

func TestRecordAggregatesPerCodeAndDay(t *testing.T) {
	st := &fakeStore{}
	c, now := newTest(st)
	c.Record("a")
	c.Record("a")
	c.Record("b")
	*now = now.Add(2 * time.Minute) // past midnight UTC
	c.Record("a")

	if err := c.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []store.ClickCount{{Code: "a", Day: "2026-10-24", Clicks: 2}, {Code: "b", Day: "2026-10-24", Clicks: 1}, {Code: "a", Day: "2026-10-25", Clicks: 1}}
	if len(st.batches) != 1 || !equal(st.batches[0], want) {
		t.Fatalf("batches = %+v, want %+v", st.batches, want)
	}
	// Flushed counts are gone from memory.
	_ = c.Flush(context.Background())
	if len(st.batches) != 1 {
		t.Fatalf("second flush wrote %+v", st.batches[1:])
	}
}

func TestFlushErrorKeepsCounts(t *testing.T) {
	st := &fakeStore{fail: errors.New("db down")}
	c, _ := newTest(st)
	c.Record("a")
	c.Record("a")
	if err := c.Flush(context.Background()); err == nil {
		t.Fatal("store error not returned")
	}
	c.Record("a")
	if err := c.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(st.batches) != 1 || st.batches[0][0].Clicks != 3 {
		t.Fatalf("batches = %+v, want a=3 after the retry", st.batches)
	}
}

func TestCapDropsNewKeysOnly(t *testing.T) {
	st := &fakeStore{}
	c, _ := newTest(st)
	c.maxKeys = 2
	c.Record("a")
	c.Record("b")
	c.Record("c") // over the cap: dropped
	c.Record("a") // existing key: still counted
	_ = c.Flush(context.Background())
	want := []store.ClickCount{{Code: "a", Day: "2026-10-24", Clicks: 2}, {Code: "b", Day: "2026-10-24", Clicks: 1}}
	if !equal(st.batches[0], want) {
		t.Fatalf("batch = %+v, want %+v", st.batches[0], want)
	}
}

func TestRunFlushesOnStop(t *testing.T) {
	st := &fakeStore{}
	c, _ := newTest(st)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.Run(ctx, time.Hour)
		close(done)
	}()
	c.Record("a")
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
	if len(st.batches) != 1 || st.batches[0][0].Code != "a" {
		t.Fatalf("pending clicks not flushed on stop: %+v", st.batches)
	}
}

func equal(a, b []store.ClickCount) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
