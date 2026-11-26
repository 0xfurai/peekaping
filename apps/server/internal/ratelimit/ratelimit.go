// Package ratelimit counts requests per key in a sliding window, in memory.
//
// The counts live in the memory of one process. An install that runs several API replicas
// therefore allows each replica's share: the limit is a guard against a script, not an exact
// quota. A restart forgets the counts.
package ratelimit

import (
	"sync"
	"time"
)

// defaultMaxKeys bounds the memory a flood of distinct keys can take. At the cap the expired keys
// are dropped first; if the counts still hold that many keys, a key that is new is refused (counted
// as limited). The counts are never wiped: an attacker who sends many addresses must not be able to
// reset the count of the one they are after.
const defaultMaxKeys = 100_000

// capSweepEvery is the least time between two sweeps that a full limiter makes for a new key, so
// that a flood of new keys cannot make every request walk all the counts
const capSweepEvery = time.Second

// Limiter allows at most max hits per key in any window
type Limiter struct {
	max     int
	window  time.Duration
	maxKeys int
	now     func() time.Time

	mu        sync.Mutex
	hits      map[string][]time.Time
	lastSweep time.Time
}

// New returns a limiter that allows max hits per key in a window
func New(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, maxKeys: defaultMaxKeys, now: time.Now, hits: map[string][]time.Time{}}
}

// WithClock replaces the clock, for tests
func (l *Limiter) WithClock(now func() time.Time) *Limiter {
	l.now = now
	return l
}

// WithMaxKeys replaces the cap on the number of keys, for tests
func (l *Limiter) WithMaxKeys(n int) *Limiter {
	l.maxKeys = n
	return l
}

// Allow records a hit for the key and reports whether it is within the limit. A hit that is over
// the limit is not recorded, so a caller that keeps trying gets in as soon as the oldest hit
// leaves the window. retryAfter is how long that takes, zero when the hit was allowed.
func (l *Limiter) Allow(key string) (allowed bool, retryAfter time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)

	recent, known := l.hits[key]
	if !known && len(l.hits) >= l.maxKeys {
		// Full: make room from the expired keys, and when there is none, refuse the new key
		if now.Sub(l.lastSweep) >= capSweepEvery {
			l.drop(now)
		}
		if len(l.hits) >= l.maxKeys {
			return false, l.window
		}
	}
	cut := 0
	for cut < len(recent) && !recent[cut].After(now.Add(-l.window)) {
		cut++
	}
	recent = recent[cut:]
	if len(recent) >= l.max {
		l.hits[key] = recent
		return false, recent[0].Add(l.window).Sub(now)
	}
	l.hits[key] = append(recent, now)
	return true, 0
}

// sweep drops the keys whose hits have all left the window, at most once a minute
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.drop(now)
}

func (l *Limiter) drop(now time.Time) {
	l.lastSweep = now
	for key, hits := range l.hits {
		if len(hits) == 0 || !hits[len(hits)-1].After(now.Add(-l.window)) {
			delete(l.hits, key)
		}
	}
}
