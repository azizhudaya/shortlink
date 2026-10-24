// Package ratelimit is a keyed token-bucket limiter for burst control (D12).
// It lives in memory: a restart resets it, which is acceptable because
// durable daily volume is the quota's job (D24).
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type Limiter struct {
	every   time.Duration
	burst   int
	idleTTL time.Duration
	maxKeys int
	now     func() time.Time

	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	lim  *rate.Limiter
	last time.Time
}

// New allows burst requests per key, refilling one every `every`. Keys idle
// for idleTTL are evicted when the map is full; if it is still full, new keys
// are refused (fail closed) rather than letting memory grow without bound.
func New(every time.Duration, burst int, idleTTL time.Duration, maxKeys int) *Limiter {
	return &Limiter{
		every:   every,
		burst:   burst,
		idleTTL: idleTTL,
		maxKeys: maxKeys,
		now:     time.Now,
		entries: map[string]*entry{},
	}
}

// Allow reports whether the key may proceed, and if not, how long to wait.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	e, found := l.entries[key]
	if !found {
		if len(l.entries) >= l.maxKeys {
			l.evictIdle(now)
		}
		if len(l.entries) >= l.maxKeys {
			return false, l.every
		}
		e = &entry{lim: rate.NewLimiter(rate.Every(l.every), l.burst)}
		l.entries[key] = e
	}
	e.last = now

	r := e.lim.ReserveN(now, 1)
	if !r.OK() {
		return false, l.every
	}
	if d := r.DelayFrom(now); d > 0 {
		r.CancelAt(now) // a refused request must not spend a token
		return false, d
	}
	return true, 0
}

func (l *Limiter) evictIdle(now time.Time) {
	for k, e := range l.entries {
		if now.Sub(e.last) > l.idleTTL {
			delete(l.entries, k)
		}
	}
}
