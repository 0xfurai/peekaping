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

	// HelloProbeRate is how many hellos a second one probe may make, from all its instances
	// together, once it has used its burst
	HelloProbeRate = rate.Limit(1)
	// HelloProbeBurst is how many hellos a probe may make at once. Hello is the one route that
	// takes an instance id the server has not seen, so a token holder who rotates ids gets a fresh
	// instance bucket for each request; this bucket is what stops it.
	HelloProbeBurst = 10

	// limiterIdleAfter is how long an instance can go without a request to a route before the
	// limiter forgets it. A bucket that has been idle for this long is full again, so forgetting
	// it changes nothing for the instance.
	limiterIdleAfter = time.Hour
	// limiterSweepEvery is how often, at most, the limiter looks for instances to forget
	limiterSweepEvery = 10 * time.Minute

	// MessageTooManyRequests is the message of a 429
	MessageTooManyRequests = "too many requests"
)

// helloRoute is the route of a hello, as the router names it
const helloRoute = "POST " + probeproto.PathPrefix + "/hello"

// limiterKey is one bucket: a probe's instance and a route, or, for perProbe, a probe and a route
// whatever the instance. The probe is part of the key because an instance id is the probe's own to
// choose, and must not let it use up another probe's budget.
type limiterKey struct {
	probeID    string
	instanceID string
	route      string
	perProbe   bool
}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// RateLimiter limits how fast an instance may call each route of the probe protocol: a burst of
// DefaultBurst requests, then DefaultRate a second. Hello has a second limit, per probe: a burst of
// HelloProbeBurst, then HelloProbeRate a second, whichever instance says it. The routes share one
// RateLimiter, which keeps a bucket for each (probe, instance, route), and one for each probe's
// hello, in memory and forgets the buckets that have gone unused for an hour.
type RateLimiter struct {
	limit      rate.Limit
	burst      int
	retryAfter string
	now        func() time.Time

	helloLimit      rate.Limit
	helloBurst      int
	helloRetryAfter string

	mu        sync.Mutex
	entries   map[limiterKey]*limiterEntry
	lastSweep time.Time
}

// NewRateLimiter returns a limiter of DefaultBurst requests at once and DefaultRate a second
func NewRateLimiter() *RateLimiter {
	return newRateLimiter(DefaultRate, DefaultBurst, time.Now)
}

// NewRateLimiterOf returns a limiter of burst requests at once and limit a second, in place of the
// defaults, for a measurement that has to post faster than a probe does
func NewRateLimiterOf(limit rate.Limit, burst int) *RateLimiter {
	return newRateLimiter(limit, burst, time.Now)
}

func newRateLimiter(limit rate.Limit, burst int, now func() time.Time) *RateLimiter {
	return &RateLimiter{
		limit:           limit,
		burst:           burst,
		retryAfter:      retryAfterOf(limit),
		now:             now,
		helloLimit:      HelloProbeRate,
		helloBurst:      HelloProbeBurst,
		helloRetryAfter: retryAfterOf(HelloProbeRate),
		entries:         map[limiterKey]*limiterEntry{},
		lastSweep:       now(),
	}
}

// retryAfterOf is the Retry-After of a denied request: the seconds until a token has been added to
// its bucket
func retryAfterOf(limit rate.Limit) string {
	seconds := 1
	if limit > 0 && !math.IsInf(float64(limit), 0) {
		seconds = max(1, int(math.Ceil(1/float64(limit))))
	}
	return strconv.Itoa(seconds)
}

// Middleware answers 429 with a Retry-After header to a request over its instance's limit for the
// route, and to a hello over its probe's. It goes after the bearer middleware, which gives the
// request its probe and instance.
func (l *RateLimiter) Middleware() httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if retryAfter, ok := l.allow(keyOf(r)); !ok {
				w.Header().Set("Retry-After", retryAfter)
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

// allow takes a token from the instance's bucket and, for a hello, from its probe's bucket after
// it. A denied request is answered with the Retry-After of the bucket that denied it. A hello the
// instance's bucket denies takes nothing from the probe's.
func (l *RateLimiter) allow(key limiterKey) (retryAfter string, ok bool) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.take(key, l.limit, l.burst, now) {
		return l.retryAfter, false
	}
	if key.route == helloRoute && key.probeID != "" {
		perProbe := limiterKey{probeID: key.probeID, route: key.route, perProbe: true}
		if !l.take(perProbe, l.helloLimit, l.helloBurst, now) {
			return l.helloRetryAfter, false
		}
	}
	return "", true
}

// take takes a token from the bucket of the key, which it makes if there is none. The caller holds
// the lock.
func (l *RateLimiter) take(key limiterKey, limit rate.Limit, burst int, now time.Time) bool {
	entry, ok := l.entries[key]
	if !ok {
		l.sweep(now)
		entry = &limiterEntry{limiter: rate.NewLimiter(limit, burst)}
		l.entries[key] = entry
	}
	entry.lastUsed = now
	return entry.limiter.AllowN(now, 1)
}

// sweep forgets the buckets that have been idle for an hour, a probe's hello bucket among them. It
// runs when a bucket is added, which is the only time the map grows, and at most every
// limiterSweepEvery. The caller holds the lock.
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
	_, ok := l.entries[limiterKey{probeID: probeID, instanceID: instanceID, route: route}]
	return ok
}
