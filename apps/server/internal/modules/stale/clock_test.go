package stale

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestRedisClock_IsTheTimeOfTheRedisTheReplicasShare(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	redisNow := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	server.SetTime(redisNow)

	now := RedisClock(client, zap.NewNop().Sugar())()

	assert.True(t, redisNow.Equal(now), "the replica's own clock is not asked: %s", now)
}

func TestRedisClock_FallsBackToTheLocalClockWhenRedisDoesNotAnswer(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	clock := RedisClock(client, zap.NewNop().Sugar())
	server.Close()

	now := clock()

	assert.WithinDuration(t, time.Now(), now, 2*time.Second)
}
