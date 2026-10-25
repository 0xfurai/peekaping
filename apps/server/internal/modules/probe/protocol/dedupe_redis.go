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
)

// redisWindow is the window of cluster mode, shared by the API replicas through Redis: a claim is
// SET NX of the id's key with the window's hour as its TTL, and a release is DEL. Redis decides
// which of any number of claims at once wins, and when the hour is out, so the replicas need
// agree on nothing but the Redis.
type redisWindow struct {
	client *redis.Client
	ttl    time.Duration
}

var _ Window = (*redisWindow)(nil)

// NewRedisWindow is cluster mode's window, in the Redis the API replicas share
func NewRedisWindow(client *redis.Client) Window {
	return &redisWindow{client: client, ttl: acceptedTTL}
}

func (w *redisWindow) key(probeID, id string) string { return acceptedKeyPrefix + probeID + ":" + id }

// Claim implements Window. A Redis that fails to answer is an error and not a free id: taking the
// result without a record of it could store it twice, so the batch is answered 503 for the probe
// to send again.
func (w *redisWindow) Claim(ctx context.Context, probeID, id string, _ time.Time) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, redisWindowTimeout)
	defer cancel()
	claimed, err := w.client.SetNX(ctx, w.key(probeID, id), 1, w.ttl).Result()
	if err != nil {
		return false, fmt.Errorf("claim result %s of probe %s in redis: %w", id, probeID, err)
	}
	return claimed, nil
}

// Release implements Window. It ignores the cancellation of ctx: the request that gives its ids
// back is often the one that was cancelled, and an id left claimed would answer the probe's retry
// duplicate for the rest of the hour, though the result was never stored.
func (w *redisWindow) Release(ctx context.Context, probeID, id string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), redisWindowTimeout)
	defer cancel()
	if err := w.client.Del(ctx, w.key(probeID, id)).Err(); err != nil {
		return fmt.Errorf("release result %s of probe %s in redis: %w", id, probeID, err)
	}
	return nil
}
