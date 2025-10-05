package producer

import (
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis keys for scheduler
const (
	SchedDueKey   = "peekaping:sched:due"   // ZSET: score=next_due_ms, member=monitor_id
	SchedLeaseKey = "peekaping:sched:lease" // ZSET: score=lease_expire_ms, member=monitor_id

	BatchClaim   = 2                     // max items to claim per tick
	LeaseTTL     = 10 * time.Second      // how long an item can sit in "lease" while enqueuing
	ReclaimEvery = 2 * time.Second       // how often to sweep expired leases
	ClaimTick    = 25 * time.Millisecond // how often to check for due monitors
)

// Lua scripts for atomic operations
const (
	// CLAIM: move due items (score <= now_ms) from due → lease with lease expiry.
	claimLua = `
local due   = KEYS[1]
local lease = KEYS[2]
local now   = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local lms   = tonumber(ARGV[3])

local ids = redis.call('ZRANGEBYSCORE', due, '-inf', now, 'LIMIT', 0, limit)
if #ids == 0 then return ids end
for i=1,#ids do
  redis.call('ZREM', due, ids[i])
  redis.call('ZADD', lease, now + lms, ids[i])
end
return ids
`

	// RESCHEDULE: move a claimed item lease → due at next_ts_ms
	reschedLua = `
local lease = KEYS[1]
local due   = KEYS[2]
local id    = ARGV[1]
local next  = tonumber(ARGV[2])
redis.call('ZREM', lease, id)
redis.call('ZADD', due, next, id)
return 1
`

	// RECLAIM: move expired leases (score <= now_ms) back to due at now_ms
	reclaimLua = `
local lease = KEYS[1]
local due   = KEYS[2]
local now   = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])

local ids = redis.call('ZRANGEBYSCORE', lease, '-inf', now, 'LIMIT', 0, limit)
for i=1,#ids do
  redis.call('ZREM', lease, ids[i])
  redis.call('ZADD', due, now, ids[i])
end
return ids
`
)

var (
	claimScript   *redis.Script
	reclaimScript *redis.Script
)

func init() {
	claimScript = redis.NewScript(claimLua)
	reclaimScript = redis.NewScript(reclaimLua)
}
