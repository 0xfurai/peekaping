// Package probeclient is the probe's side of the probe protocol: the session that says hello to
// the server and decides what each failure means, the source that follows the probe's assignment
// over the long poll, the sink that buffers the results and posts them in batches, and the
// executors a probe runs. It speaks to the server through probeproto, and opens no database or
// Redis connection.
package probeclient

import (
	"math/rand/v2"
	"time"
)

const (
	// DefaultBackoffMin is the first wait after a failure
	DefaultBackoffMin = time.Second
	// DefaultBackoffMax is the longest wait between attempts
	DefaultBackoffMax = time.Minute
)

// Backoff is how long to wait between attempts at something the server doesn't answer: the first
// wait is Min, each next one doubles, and none passes Max. Each wait is cut by jitter to a random
// point between half its step and the step, so that probes that lost the server together don't
// come back together. The zero value is ready: one second doubling to a minute. Reset it when the
// server answers.
type Backoff struct {
	// Min is the first step; DefaultBackoffMin when zero
	Min time.Duration
	// Max is the last step; DefaultBackoffMax when zero
	Max time.Duration
	// Rand returns a number in [0, 1); math/rand/v2's when nil
	Rand func() float64

	failures int
	// conflicted is set once a 409 has been met since the server last answered. The session says
	// hello at once for the first, and backs off for the ones after it.
	conflicted bool
}

// Next returns the wait after one more failure
func (b *Backoff) Next() time.Duration {
	step, limit := b.Min, b.Max
	if step <= 0 {
		step = DefaultBackoffMin
	}
	if limit <= 0 {
		limit = DefaultBackoffMax
	}
	for range b.failures {
		step *= 2
		if step >= limit {
			break
		}
	}
	step = min(step, limit)
	b.failures++

	random := rand.Float64
	if b.Rand != nil {
		random = b.Rand
	}
	half := step / 2
	return half + time.Duration(random()*float64(step-half))
}

// Reset starts the waits over from Min, after the server has answered
func (b *Backoff) Reset() {
	b.failures = 0
	b.conflicted = false
}
