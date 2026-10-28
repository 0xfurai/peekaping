package ratelimit_test

import (
	"fmt"
	"testing"
	"time"

	"peekaping/internal/ratelimit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// At the cap of keys, a flood of new keys must not make the limiter forget the keys it holds: an
// attacker could use that to reset the count of an address they are after.
func TestLimiter_AtTheCapKeepsTheCountsAndRefusesNewKeys(t *testing.T) {
	c := &clock{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	limiter := ratelimit.New(1, time.Hour).WithClock(c.Now).WithMaxKeys(3)
	for _, key := range []string{"a", "b", "victim"} {
		allowed, _ := limiter.Allow(key)
		require.True(t, allowed, key)
	}

	allowed, retryAfter := limiter.Allow("new")
	assert.False(t, allowed, "a new key over the cap is counted as limited")
	assert.Positive(t, retryAfter)
	allowed, _ = limiter.Allow("another-new")
	assert.False(t, allowed)

	allowed, _ = limiter.Allow("victim")
	assert.False(t, allowed, "the count of a key that was there is still there")
}

func TestLimiter_AtTheCapEvictsExpiredKeysBeforeRefusingNewOnes(t *testing.T) {
	c := &clock{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	limiter := ratelimit.New(1, time.Hour).WithClock(c.Now).WithMaxKeys(3)
	for _, key := range []string{"a", "b"} {
		limiter.Allow(key)
	}
	c.now = c.now.Add(2 * time.Hour)
	limiter.Allow("c")
	limiter.Allow("d") // the expired a and b make room, even though the minute sweep has not run

	allowed, _ := limiter.Allow("e")
	assert.True(t, allowed, "c and d fit under the cap of 3 with e")
	allowed, _ = limiter.Allow("f")
	assert.False(t, allowed, "now c, d and e fill it")
}
