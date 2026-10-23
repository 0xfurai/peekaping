package producer

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestLeaderElection_NewLeaderElection(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()

	logger := zap.NewNop().Sugar()
	nodeID := "test-node-1"

	le := NewLeaderElection(client, nodeID, logger)

	assert.NotNil(t, le)
	assert.Equal(t, nodeID, le.nodeID)
	assert.False(t, le.isLeader)
	assert.Equal(t, LeaderRenewalInterval, le.renewalInterval)
	assert.NotNil(t, le.stopChan)
	assert.NotNil(t, le.doneChan)
}

func TestLeaderElection_TryBecomeLeader(t *testing.T) {
	t.Run("successfully become leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		ctx := context.Background()
		le.tryBecomeLeader(ctx)

		assert.True(t, le.IsLeader())

		// Verify Redis key is set
		value, err := client.Get(ctx, LeaderKey).Result()
		require.NoError(t, err)
		assert.Equal(t, "node1", value)
	})

	t.Run("fail to become leader when another node is leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()

		// First node becomes leader
		le1 := NewLeaderElection(client, "node1", logger)
		ctx := context.Background()
		le1.tryBecomeLeader(ctx)
		assert.True(t, le1.IsLeader())

		// Second node tries to become leader
		le2 := NewLeaderElection(client, "node2", logger)
		le2.tryBecomeLeader(ctx)
		assert.False(t, le2.IsLeader())
	})

	t.Run("renew leadership when already leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		ctx := context.Background()
		le.tryBecomeLeader(ctx)
		assert.True(t, le.IsLeader())

		// Try to become leader again (should renew)
		le.tryBecomeLeader(ctx)
		assert.True(t, le.IsLeader())
	})
}

func TestLeaderElection_ReleaseLeadership(t *testing.T) {
	t.Run("successfully release leadership", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		ctx := context.Background()
		le.tryBecomeLeader(ctx)
		assert.True(t, le.IsLeader())

		le.releaseLeadership(ctx)
		assert.False(t, le.IsLeader())

		// Verify Redis key is deleted
		_, err := client.Get(ctx, LeaderKey).Result()
		assert.Equal(t, redis.Nil, err)
	})

	// A renewal that is under way when the election's context is cancelled fails on the context.
	// That says nothing about the leadership: the release that follows still has to delete the key.
	t.Run("release leadership after a renewal failed on a cancelled context", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		le := NewLeaderElection(client, "node1", zap.NewNop().Sugar())
		le.tryBecomeLeader(context.Background())
		require.True(t, le.IsLeader())

		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		le.tryBecomeLeader(cancelled)
		assert.True(t, le.IsLeader(), "a cancelled renewal made the node forget its leadership")

		le.releaseLeadership(context.Background())
		assert.False(t, mr.Exists(LeaderKey))
	})

	t.Run("release leadership when not leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		ctx := context.Background()
		// Don't become leader, just try to release
		le.releaseLeadership(ctx)
		assert.False(t, le.IsLeader())
	})

	t.Run("don't delete key if another node is leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()

		// Node 1 becomes leader
		le1 := NewLeaderElection(client, "node1", logger)
		ctx := context.Background()
		le1.tryBecomeLeader(ctx)
		assert.True(t, le1.IsLeader())

		// Node 2 tries to release (but it's not the leader)
		le2 := NewLeaderElection(client, "node2", logger)
		le2.isLeader = true // Force it to think it's leader
		le2.releaseLeadership(ctx)

		// Verify node1 is still the leader
		value, err := client.Get(ctx, LeaderKey).Result()
		require.NoError(t, err)
		assert.Equal(t, "node1", value)
	})
}

func TestLeaderElection_WaitForLeadership(t *testing.T) {
	t.Run("returns immediately when already leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		ctx := context.Background()
		le.tryBecomeLeader(ctx)
		assert.True(t, le.IsLeader())

		err := le.WaitForLeadership(ctx)
		assert.NoError(t, err)
	})

	t.Run("returns error when context is cancelled", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := le.WaitForLeadership(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context cancelled")
	})

	t.Run("waits and returns when becomes leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		ctx := context.Background()

		// Start waiting in a goroutine
		errChan := make(chan error, 1)
		go func() {
			errChan <- le.WaitForLeadership(ctx)
		}()

		// It waits while this node is a follower
		assert.Never(t, func() bool { return len(errChan) > 0 }, 20*time.Millisecond, time.Millisecond)

		le.tryBecomeLeader(ctx)

		// Should complete without error
		select {
		case err := <-errChan:
			assert.NoError(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("WaitForLeadership timed out")
		}
	})
}

// newElection is a node's leader election, renewing every few milliseconds.
func newElection(client *redis.Client, nodeID string) *LeaderElection {
	le := NewLeaderElection(client, nodeID, zap.NewNop().Sugar())
	le.renewalInterval = 2 * time.Millisecond
	return le
}

func leaderHolder(t *testing.T, client *redis.Client) string {
	t.Helper()
	holder, err := client.Get(context.Background(), LeaderKey).Result()
	require.NoError(t, err)
	return holder
}

func TestLeaderElection_StartStop(t *testing.T) {
	ctx := context.Background()

	t.Run("start and stop leader election", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		le := newElection(client, "node1")

		le.Start(ctx)
		require.Eventually(t, le.IsLeader, time.Second, time.Millisecond)

		le.Stop()

		// Stopping releases the lock
		assert.False(t, le.IsLeader())
		assert.False(t, mr.Exists(LeaderKey))
	})

	t.Run("multiple nodes competing for leadership", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		le1 := newElection(client, "node1")
		le2 := newElection(client, "node2")

		le1.Start(ctx)
		le2.Start(ctx)
		defer le1.Stop()
		defer le2.Stop()

		require.Eventually(t, func() bool { return le1.IsLeader() || le2.IsLeader() }, time.Second, time.Millisecond)
		// Renewals later there's still exactly one leader, and it holds the lock
		assert.Never(t, func() bool { return le1.IsLeader() == le2.IsLeader() }, 20*time.Millisecond, time.Millisecond)
		if le1.IsLeader() {
			assert.Equal(t, "node1", leaderHolder(t, client))
		} else {
			assert.Equal(t, "node2", leaderHolder(t, client))
		}
	})

	t.Run("leadership transfer on stop", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		le1 := newElection(client, "node1")
		le2 := newElection(client, "node2")

		le1.Start(ctx)
		require.Eventually(t, le1.IsLeader, time.Second, time.Millisecond)
		le2.Start(ctx)
		defer le2.Stop()
		assert.Never(t, le2.IsLeader, 20*time.Millisecond, time.Millisecond)

		// node1 releases the lock as it stops, so node2 takes it on its next renewal
		le1.Stop()

		require.Eventually(t, le2.IsLeader, time.Second, time.Millisecond)
		assert.Equal(t, "node2", leaderHolder(t, client))
	})

	t.Run("leadership transfer when the leader's lock expires", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		// A leader that crashed keeps the lock until it expires
		require.NoError(t, client.Set(ctx, LeaderKey, "node-crashed", LeaderTTL).Err())
		le := newElection(client, "node1")

		le.Start(ctx)
		defer le.Stop()
		assert.Never(t, le.IsLeader, 20*time.Millisecond, time.Millisecond)

		mr.FastForward(LeaderTTL)

		require.Eventually(t, le.IsLeader, time.Second, time.Millisecond)
		assert.Equal(t, "node1", leaderHolder(t, client))
	})
}

func TestLeaderElection_SetLeaderStatus(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()

	logger := zap.NewNop().Sugar()
	le := NewLeaderElection(client, "node1", logger)

	assert.False(t, le.IsLeader())

	le.setLeaderStatus(true)
	assert.True(t, le.IsLeader())

	le.setLeaderStatus(false)
	assert.False(t, le.IsLeader())
}
