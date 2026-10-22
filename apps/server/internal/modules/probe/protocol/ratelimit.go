package protocol

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/probeproto"
	"peekaping/internal/utils"

	"golang.org/x/time/rate"
)

const (
	// DefaultRate is how many requests a second one instance may make to one route, once it
	// has used its burst
	DefaultRate = rate.Limit(1)
	// DefaultBurst is how many requests an instance may make at once
	DefaultBurst = 5

	// limiterIdleAfter is how long an instance can go without a request to a route before the
	// limiter forgets it. A bucket that has been idle for this long is full again, so forgetting
	// it changes nothing for the instance.
	limiterIdleAfter = time.Hour
	// limiterSweepEvery is how often, at most, the limiter looks for instances to forget
	limiterSweepEvery = 10 * time.Minute

	// MessageTooManyRequests is the message of a 429
	MessageTooManyRequests = "too many requests"
)

// limiterKey is one bucket: a probe's instance and a route. The probe is part of the key because
// an instance id is the probe's own to choose, and must not let it use up another probe's budget.
type limiterKey struct {
	probeID    string
	instanceID string
	route      string
}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// RateLimiter limits how fast an instance may call each route of the probe protocol: a burst of
// DefaultBurst requests, then DefaultRate a second. The routes share one RateLimiter, which keeps
// a bucket for each (probe, instance, route) in memory and forgets the buckets that have gone
// unused for an hour.
type RateLimiter struct {
	limit      rate.Limit
	burst      int
	retryAfter string
	now        func() time.Time

	mu        sync.Mutex
	entries   map[limiterKey]*limiterEntry
	lastSweep time.Time
}

// NewRateLimiter returns a limiter of DefaultBurst requests at once and DefaultRate a second
func NewRateLimiter() *RateLimiter {
	return newRateLimiter(DefaultRate, DefaultBurst, time.Now)
}

func newRateLimiter(limit rate.Limit, burst int, now func() time.Time) *RateLimiter {
	// A denied request is told to come back when a token has been added to its bucket
	retryAfter := 1
	if limit > 0 && !math.IsInf(float64(limit), 0) {
		retryAfter = max(1, int(math.Ceil(1/float64(limit))))
	}
	return &RateLimiter{
		limit:      limit,
		burst:      burst,
		retryAfter: strconv.Itoa(retryAfter),
		now:        now,
		entries:    map[limiterKey]*limiterEntry{},
		lastSweep:  now(),
	}
}

// Middleware answers 429 with a Retry-After header to a request over its instance's limit for the
// route. It goes after the bearer middleware, which gives the request its probe and instance.
func (l *RateLimiter) Middleware() httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.allow(keyOf(r)) {
				w.Header().Set("Retry-After", l.retryAfter)
				httpx.JSON(w, http.StatusTooManyRequests, utils.NewFailResponse(MessageTooManyRequests))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// keyOf is the bucket of a request: the route the router matched, or else the request's own method
// and path
func keyOf(r *http.Request) limiterKey {
	route := r.Pattern
	if route == "" {
		route = r.Method + " " + r.URL.Path
	}
	key := limiterKey{route: route}
	if p, instanceID, ok := ProbeFromContext(r.Context()); ok {
		key.probeID, key.instanceID = p.ID, instanceID
	} else {
		key.instanceID = r.Header.Get(probeproto.HeaderInstanceID)
	}
	return key
}

func (l *RateLimiter) allow(key limiterKey) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[key]
	if !ok {
		l.sweep(now)
		entry = &limiterEntry{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.entries[key] = entry
	}
	entry.lastUsed = now
	return entry.limiter.AllowN(now, 1)
}

// sweep forgets the buckets that have been idle for an hour. It runs when a bucket is added, which
// is the only time the map grows, and at most every limiterSweepEvery. The caller holds the lock.
func (l *RateLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < limiterSweepEvery {
		return
	}
	l.lastSweep = now
	for key, entry := range l.entries {
		if now.Sub(entry.lastUsed) >= limiterIdleAfter {
			delete(l.entries, key)
		}
	}
}

// size is how many buckets the limiter keeps
func (l *RateLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// has reports whether the limiter keeps the bucket of a route for an instance of a probe
func (l *RateLimiter) has(probeID, instanceID, route string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.entries[limiterKey{probeID, instanceID, route}]
	return ok
}
