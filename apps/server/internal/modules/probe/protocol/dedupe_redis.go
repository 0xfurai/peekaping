package protocol

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// acceptedKeyPrefix is the start of the Redis key of a result id the window holds: the key is
	// this, the probe's id and the result's id. A probe's id is a UUID, or "local", and has no
	// colon, so two probes' ids never make one key.
	acceptedKeyPrefix = "peekaping:probe:accepted:"
	// redisWindowTimeout bounds a call to Redis, and a release that runs after its request ended
	redisWindowTimeout = 5 * time.Second
	// pendingValue is the value of a claim whose result is not stored yet, and storedValue the
	// value of an id that was kept. A key of any other value (an older server wrote 1) is read as
	// stored: the safe reading, which answers duplicate and stores nothing twice.
	pendingValue = "pending"
	storedValue  = "stored"
	// wonReply is what claimScript answers when the claim won; anything else is the value held
	wonReply = "won"
)

// releaseScript deletes a key only while it still holds a claim that was not kept. A request that
// ran past its claim finds it gone, and perhaps claimed, stored and kept by another request since:
// that stored mark is not its to remove. KEYS[1] is the id's key and ARGV[1] the value of a claim.
var releaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

// claimScript claims a key if it is free, and otherwise reports the value it holds, in one round
// trip and with nothing between the read and the write: KEYS[1] is the id's key, ARGV[1] the value
// of a claim and ARGV[2] its time to live in milliseconds
var claimScript = redis.NewScript(`
local held = redis.call('GET', KEYS[1])
if held == false then
  redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
  return '` + wonReply + `'
end
return held
`)

// redisWindow is the window of cluster mode, shared by the API replicas through Redis: a claim is
// a script that sets the id's key to "pending" with ClaimTTL as its TTL if it is free, and reports
// what the key holds if it is not. Keeping an id is SET of the key to "stored" with the window's
// hour, and a release is DEL. Redis decides which of any number of claims at once wins, and when
// each runs out, so the replicas need agree on nothing but the Redis.
//
// A claim is short because the request that made it may die, and then nothing gives it back: the
// key would stand for the hour. A request that finds a pending key is not told the result is a
// duplicate, which the probe drops for good: the holder may have died, and the result never be
// stored. It is told to come back, and finds the key stored if the holder lived, and gone if not.
// The request keeps its ids once the results are stored.
type redisWindow struct {
	client *redis.Client
	// claimTTL is how long a claim stands that is neither kept nor released
	claimTTL time.Duration
	// ttl is how long a kept id stands
	ttl time.Duration
}

var _ Window = (*redisWindow)(nil)

// NewRedisWindow is cluster mode's window, in the Redis the API replicas share
func NewRedisWindow(client *redis.Client) Window {
	return &redisWindow{client: client, claimTTL: ClaimTTL, ttl: acceptedTTL}
}

func (w *redisWindow) key(probeID, id string) string { return acceptedKeyPrefix + probeID + ":" + id }

// Claim implements Window. A Redis that fails to answer is an error and not a free id: taking the
// result without a record of it could store it twice, so the batch is answered 503 for the probe
// to send again.
func (w *redisWindow) Claim(ctx context.Context, probeID, id string, _ time.Time) (ClaimOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, redisWindowTimeout)
	defer cancel()
	held, err := claimScript.Run(ctx, w.client, []string{w.key(probeID, id)}, pendingValue, w.claimTTL.Milliseconds()).Text()
	if err != nil {
		return 0, fmt.Errorf("claim result %s of probe %s in redis: %w", id, probeID, err)
	}
	switch held {
	case wonReply:
		return ClaimWon, nil
	case pendingValue:
		return ClaimPending, nil
	default:
		return ClaimStored, nil
	}
}

// Keep implements Window. It sets the keys outright rather than extending them: a request that ran
// longer than ClaimTTL finds its claims gone, and the results are stored all the same. The ids go
// out in one pipeline. Like Release, it ignores the cancellation of ctx.
func (w *redisWindow) Keep(ctx context.Context, probeID string, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), redisWindowTimeout)
	defer cancel()
	pipe := w.client.Pipeline()
	for _, id := range ids {
		pipe.Set(ctx, w.key(probeID, id), storedValue, w.ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("keep %d result ids of probe %s in redis: %w", len(ids), probeID, err)
	}
	return nil
}

// Release implements Window. It ignores the cancellation of ctx: the request that gives its ids
// back is often the one that was cancelled, and an id left claimed would hold the probe's retry
// off until the claim ran out, though the result was never stored. It deletes the key only while
// it is still a claim: an id that was kept stays kept.
func (w *redisWindow) Release(ctx context.Context, probeID, id string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), redisWindowTimeout)
	defer cancel()
	if err := releaseScript.Run(ctx, w.client, []string{w.key(probeID, id)}, pendingValue).Err(); err != nil {
		return fmt.Errorf("release result %s of probe %s in redis: %w", id, probeID, err)
	}
	return nil
}
