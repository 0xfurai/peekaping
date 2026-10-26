package probeclient_test

import (
	"testing"
	"time"

	"peekaping/internal/probeclient"

	"github.com/stretchr/testify/assert"
)

func TestBackoff_DoublesTo60sWithJitterAndResets(t *testing.T) {
	bases := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60, 60}

	t.Run("each wait is between half its step and the step", func(t *testing.T) {
		var backoff probeclient.Backoff
		for i, base := range bases {
			base *= time.Second

			wait := backoff.Next()

			assert.GreaterOrEqual(t, wait, base/2, "wait %d", i+1)
			assert.LessOrEqual(t, wait, base, "wait %d", i+1)
		}
	})

	t.Run("the jitter spreads the waits of probes that fail together", func(t *testing.T) {
		seen := map[time.Duration]bool{}
		for range 20 {
			var backoff probeclient.Backoff
			seen[backoff.Next()] = true
		}

		assert.Greater(t, len(seen), 1, "twenty first waits are not all the same")
	})

	t.Run("a reset starts again from one second", func(t *testing.T) {
		var backoff probeclient.Backoff
		for range 5 {
			backoff.Next()
		}

		backoff.Reset()

		assert.LessOrEqual(t, backoff.Next(), time.Second)
	})

	t.Run("the extremes of the jitter are half the step and the step", func(t *testing.T) {
		low := probeclient.Backoff{Rand: func() float64 { return 0 }}
		high := probeclient.Backoff{Rand: func() float64 { return 1 }}

		assert.Equal(t, 500*time.Millisecond, low.Next())
		assert.Equal(t, time.Second, high.Next())
		assert.Equal(t, time.Second, low.Next())
		assert.Equal(t, 2*time.Second, high.Next())
	})
}
