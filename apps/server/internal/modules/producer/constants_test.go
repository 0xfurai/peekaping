package producer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConstants(t *testing.T) {
	t.Run("Redis keys are defined", func(t *testing.T) {
		assert.Equal(t, "peekaping:sched:due", SchedDueKey)
		assert.Equal(t, "peekaping:sched:lease", SchedLeaseKey)
	})

	t.Run("timing constants are reasonable", func(t *testing.T) {
		assert.Greater(t, BatchClaim, 0)
		assert.Greater(t, LeaseTTL, time.Duration(0))
		assert.Greater(t, ReclaimEvery, time.Duration(0))
		assert.Greater(t, ClaimTick, time.Duration(0))
		assert.Greater(t, ConcurrentProducers, 0)
	})

	t.Run("lease TTL is longer than reclaim interval", func(t *testing.T) {
		assert.Greater(t, LeaseTTL, ReclaimEvery)
	})

	t.Run("leader election constants", func(t *testing.T) {
		assert.Equal(t, "peekaping:producer:leader", LeaderKey)
		assert.Greater(t, LeaderTTL, time.Duration(0))
		assert.Greater(t, LeaderRenewalInterval, time.Duration(0))
		assert.Less(t, LeaderRenewalInterval, LeaderTTL, "Renewal interval should be less than TTL")
	})
}

func TestLuaScripts(t *testing.T) {
	t.Run("Lua scripts are defined", func(t *testing.T) {
		assert.NotEmpty(t, claimLua)
		assert.NotEmpty(t, reschedLua)
		assert.NotEmpty(t, reclaimLua)
	})

	t.Run("Redis scripts are initialized", func(t *testing.T) {
		assert.NotNil(t, claimScript)
		assert.NotNil(t, reclaimScript)
	})

	t.Run("Lua scripts contain expected operations", func(t *testing.T) {
		// Claim script should handle due -> lease transition
		assert.Contains(t, claimLua, "ZRANGEBYSCORE")
		assert.Contains(t, claimLua, "ZREM")
		assert.Contains(t, claimLua, "ZADD")

		// Reschedule script should move from lease -> due
		assert.Contains(t, reschedLua, "ZREM")
		assert.Contains(t, reschedLua, "ZADD")

		// Reclaim script should move expired leases back to due
		assert.Contains(t, reclaimLua, "ZRANGEBYSCORE")
		assert.Contains(t, reclaimLua, "ZREM")
		assert.Contains(t, reclaimLua, "ZADD")
	})
}
