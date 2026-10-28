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

// maxKeys bounds the memory a flood of distinct keys can take. When the counts hold more keys
// than this after the expired ones are dropped, they are all forgotten: a limit that lets a few
// requests through is better than a process that runs out of memory.
const maxKeys = 100_000

// Limiter allows at most max hits per key in any window
type Limiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu        sync.Mutex
	hits      map[string][]time.Time
	lastSweep time.Time
}

// New returns a limiter that allows max hits per key in a window
func New(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, now: time.Now, hits: map[string][]time.Time{}}
}

// WithClock replaces the clock, for tests
func (l *Limiter) WithClock(now func() time.Time) *Limiter {
	l.now = now
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

	recent := l.hits[key]
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
	l.lastSweep = now
	for key, hits := range l.hits {
		if len(hits) == 0 || !hits[len(hits)-1].After(now.Add(-l.window)) {
			delete(l.hits, key)
		}
	}
	if len(l.hits) > maxKeys {
		l.hits = map[string][]time.Time{}
	}
}
