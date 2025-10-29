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

		// Wait a bit, then make it leader
		time.Sleep(100 * time.Millisecond)
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

func TestLeaderElection_StartStop(t *testing.T) {
	t.Run("start and stop leader election", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		ctx := context.Background()
		le.Start(ctx)

		// Wait for it to potentially become leader
		time.Sleep(100 * time.Millisecond)

		// Stop the election
		le.Stop()

		// Verify it's no longer leader
		assert.False(t, le.IsLeader())
	})

	t.Run("multiple nodes competing for leadership", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()

		le1 := NewLeaderElection(client, "node1", logger)
		le2 := NewLeaderElection(client, "node2", logger)

		ctx := context.Background()

		// Start both
		le1.Start(ctx)
		le2.Start(ctx)

		// Wait for election
		time.Sleep(200 * time.Millisecond)

		// One should be leader, the other should not
		leaders := 0
		if le1.IsLeader() {
			leaders++
		}
		if le2.IsLeader() {
			leaders++
		}

		assert.Equal(t, 1, leaders, "Exactly one node should be leader")

		// Stop both
		le1.Stop()
		le2.Stop()
	})

	t.Run("leadership transfer on stop", func(t *testing.T) {
		t.Skip("Skipping leadership transfer test - timing dependent")
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()

		le1 := NewLeaderElection(client, "node1", logger)
		le2 := NewLeaderElection(client, "node2", logger)

		ctx := context.Background()

		// Start node1 first
		le1.Start(ctx)
		time.Sleep(100 * time.Millisecond)
		assert.True(t, le1.IsLeader())

		// Start node2
		le2.Start(ctx)
		time.Sleep(100 * time.Millisecond)
		assert.False(t, le2.IsLeader())

		// Stop node1
		le1.Stop()

		// Give node2 time to detect leadership is available and claim it
		// Leader renewal interval is 5 seconds
		time.Sleep(7 * time.Second)

		// Node2 should become leader
		assert.True(t, le2.IsLeader())

		// Cleanup
		le2.Stop()
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
