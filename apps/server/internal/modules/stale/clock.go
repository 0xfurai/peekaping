package stale

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// clockTimeout bounds the read of Redis's time
const clockTimeout = 5 * time.Second

// RedisClock is the clock of a watchdog in cluster mode: the time of the Redis the replicas share,
// so that whichever replica holds the leader lock measures staleness by the same clock. It falls
// back to the local clock when Redis does not answer, and says so in the log.
func RedisClock(client *redis.Client, logger *zap.SugaredLogger) func() time.Time {
	return func() time.Time {
		ctx, cancel := context.WithTimeout(context.Background(), clockTimeout)
		defer cancel()
		now, err := client.Time(ctx).Result()
		if err != nil {
			logger.Warnw("Failed to get Redis time, using local time", "error", err)
			return time.Now().UTC()
		}
		return now.UTC()
	}
}
