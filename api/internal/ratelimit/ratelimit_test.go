package ratelimit

import (
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTest(every time.Duration, burst, maxKeys int) (*Limiter, *clock) {
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	l := New(every, burst, time.Hour, maxKeys)
	l.now = c.now
	return l, c
}

func TestBurstThenRefused(t *testing.T) {
	l, c := newTest(time.Minute, 3, 10)
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("k"); !ok {
			t.Fatalf("request %d of burst refused", i+1)
		}
	}
	ok, retry := l.Allow("k")
	if ok || retry <= 0 || retry > time.Minute {
		t.Fatalf("burst+1: ok %v, retryAfter %v; want refused within a minute", ok, retry)
	}
	// A refused request must not consume a token: one interval later, one more fits.
	c.advance(time.Minute)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("refill refused")
	}
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("refill allowed more than one")
	}
}

func TestKeysIndependent(t *testing.T) {
	l, _ := newTest(time.Minute, 1, 10)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("a refused")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("b refused because of a")
	}
}

func TestIdleKeysEvictedWhenFull(t *testing.T) {
	l, c := newTest(time.Second, 1, 2)
	l.Allow("a")
	l.Allow("b")
	c.advance(2 * time.Hour)
	if ok, _ := l.Allow("c"); !ok {
		t.Fatal("new key refused although every existing key was idle")
	}
}

func TestFullMapFailsClosed(t *testing.T) {
	l, _ := newTest(time.Second, 1, 2)
	l.Allow("a")
	l.Allow("b")
	ok, retry := l.Allow("c")
	if ok || retry <= 0 {
		t.Fatalf("full map: ok %v, retryAfter %v; want refused", ok, retry)
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a's own limit ignored")
	}
}
