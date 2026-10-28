package ratelimit_test

import (
	"fmt"
	"testing"
	"time"

	"peekaping/internal/ratelimit"

	"github.com/stretchr/testify/assert"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func TestLimiter_AllowsTheMaximumPerKeyInTheWindow(t *testing.T) {
	c := &clock{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	limiter := ratelimit.New(5, time.Hour).WithClock(c.Now)

	for i := range 5 {
		allowed, _ := limiter.Allow("a@example.com")
		assert.True(t, allowed, "hit %d", i+1)
	}
	allowed, retryAfter := limiter.Allow("a@example.com")
	assert.False(t, allowed, "the sixth hit")
	assert.Equal(t, time.Hour, retryAfter)

	other, _ := limiter.Allow("b@example.com")
	assert.True(t, other, "another key has its own count")
}

func TestLimiter_AllowsAgainOnceTheOldestHitLeavesTheWindow(t *testing.T) {
	c := &clock{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	limiter := ratelimit.New(2, time.Hour).WithClock(c.Now)
	limiter.Allow("k")
	c.now = c.now.Add(30 * time.Minute)
	limiter.Allow("k")

	allowed, retryAfter := limiter.Allow("k")
	assert.False(t, allowed)
	assert.Equal(t, 30*time.Minute, retryAfter)

	c.now = c.now.Add(30 * time.Minute)
	allowed, _ = limiter.Allow("k")
	assert.True(t, allowed, "the first hit is an hour old")
	allowed, _ = limiter.Allow("k")
	assert.False(t, allowed, "the second is still in the window, and the third filled it")
}

func TestLimiter_ForgetsKeysWhoseHitsExpired(t *testing.T) {
	c := &clock{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	limiter := ratelimit.New(1, time.Hour).WithClock(c.Now)
	for i := range 50 {
		limiter.Allow(fmt.Sprintf("k%d", i))
	}

	c.now = c.now.Add(2 * time.Hour)
	allowed, _ := limiter.Allow("fresh")

	assert.True(t, allowed)
}
