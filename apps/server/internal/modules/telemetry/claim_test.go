package telemetry

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// replica is one api process: its own Redis connection, the same Redis
func replica(t *testing.T, redisServer *miniredis.Miniredis) Claimer {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisClaimer(client)
}

func TestClaim_OneReplica(t *testing.T) {
	redisServer := miniredis.RunT(t)
	first, second := replica(t, redisServer), replica(t, redisServer)
	ctx := context.Background()

	won, err := first.Claim(ctx)
	require.NoError(t, err)
	assert.True(t, won)
	won, err = second.Claim(ctx)
	require.NoError(t, err)
	assert.False(t, won, "the second replica finds the day claimed")
	won, err = first.Claim(ctx)
	require.NoError(t, err)
	assert.False(t, won, "and so does the first, when it asks again")
}

func TestClaim_TwoReplicasAskingAtOnceOneWins(t *testing.T) {
	redisServer := miniredis.RunT(t)
	replicas := []Claimer{replica(t, redisServer), replica(t, redisServer), replica(t, redisServer), replica(t, redisServer)}
	var wins atomic.Int32
	var wg sync.WaitGroup

	for _, claimer := range replicas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if won, err := claimer.Claim(context.Background()); err == nil && won {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), wins.Load())
}

func TestClaim_LastsTwentyThreeHours(t *testing.T) {
	redisServer := miniredis.RunT(t)
	first, second := replica(t, redisServer), replica(t, redisServer)
	ctx := context.Background()
	won, err := first.Claim(ctx)
	require.NoError(t, err)
	require.True(t, won)

	redisServer.FastForward(22*time.Hour + 59*time.Minute)
	won, err = second.Claim(ctx)
	require.NoError(t, err)
	assert.False(t, won, "the claim is still held")

	redisServer.FastForward(2 * time.Minute)
	won, err = second.Claim(ctx)
	require.NoError(t, err)
	assert.True(t, won, "after 23 hours the next day's ping can be claimed")
}

func TestClaim_FailsWhenRedisIsDown(t *testing.T) {
	redisServer := miniredis.RunT(t)
	claimer := replica(t, redisServer)
	redisServer.Close()

	won, err := claimer.Claim(context.Background())

	assert.Error(t, err)
	assert.False(t, won)
}

func TestAlwaysClaimer_SingleModeAlwaysWins(t *testing.T) {
	for range 3 {
		won, err := AlwaysClaimer{}.Claim(context.Background())
		require.NoError(t, err)
		assert.True(t, won)
	}
}
