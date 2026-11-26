package telemetry

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Claimer decides which process sends the day's ping. A ping is sent only after Claim says yes.
type Claimer interface {
	// Claim reports whether this process may send the day's ping
	Claim(ctx context.Context) (bool, error)
}

// AlwaysClaimer is single mode's claimer: the one process always sends, and needs no Redis.
type AlwaysClaimer struct{}

func (AlwaysClaimer) Claim(context.Context) (bool, error) { return true, nil }

const (
	// ClaimKey is the Redis key the api replicas race for
	ClaimKey = "peekaping:telemetry:ping"
	// ClaimTTL is how long a claim holds. It is shorter than the 24 hours between two pings, so
	// the claim has expired by the time the next ping is due.
	ClaimTTL = 23 * time.Hour
)

// RedisClaimer is cluster mode's claimer: the first api replica to set the key sends the ping,
// and the others find it taken (Redis SET NX, as the leader election does).
type RedisClaimer struct {
	client *redis.Client
}

func NewRedisClaimer(client *redis.Client) *RedisClaimer {
	return &RedisClaimer{client: client}
}

func (c *RedisClaimer) Claim(ctx context.Context) (bool, error) {
	return c.client.SetNX(ctx, ClaimKey, "1", ClaimTTL).Result()
}
