package leader

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestElector_New(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()

	logger := zap.NewNop().Sugar()
	nodeID := "test-node-1"

	le := New(client, nodeID, logger)

	assert.NotNil(t, le)
	assert.Equal(t, nodeID, le.nodeID)
	assert.False(t, le.IsLeader())
	assert.Equal(t, RenewalInterval, le.renewalInterval)
	assert.NotNil(t, le.stopChan)
	assert.NotNil(t, le.doneChan)
}

func TestElector_TryBecomeLeader(t *testing.T) {
	t.Run("successfully become leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := New(client, "node1", logger)

		ctx := context.Background()
		le.tryBecomeLeader(ctx)

		assert.True(t, le.IsLeader())

		// Verify Redis key is set
		value, err := client.Get(ctx, Key).Result()
		require.NoError(t, err)
		assert.Equal(t, "node1", value)
	})

	t.Run("fail to become leader when another node is leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()

		// First node becomes leader
		le1 := New(client, "node1", logger)
		ctx := context.Background()
		le1.tryBecomeLeader(ctx)
		assert.True(t, le1.IsLeader())

		// Second node tries to become leader
		le2 := New(client, "node2", logger)
		le2.tryBecomeLeader(ctx)
		assert.False(t, le2.IsLeader())
	})

	t.Run("renew leadership when already leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := New(client, "node1", logger)

		ctx := context.Background()
		le.tryBecomeLeader(ctx)
		assert.True(t, le.IsLeader())

		// Try to become leader again (should renew)
		le.tryBecomeLeader(ctx)
		assert.True(t, le.IsLeader())
	})
}

func TestElector_ReleaseLeadership(t *testing.T) {
	t.Run("successfully release leadership", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := New(client, "node1", logger)

		ctx := context.Background()
		le.tryBecomeLeader(ctx)
		assert.True(t, le.IsLeader())

		le.releaseLeadership(ctx)
		assert.False(t, le.IsLeader())

		// Verify Redis key is deleted
		_, err := client.Get(ctx, Key).Result()
		assert.Equal(t, redis.Nil, err)
	})

	// A renewal that is under way when the election's context is cancelled fails on the context.
	// That says nothing about the leadership: the release that follows still has to delete the key.
	t.Run("release leadership after a renewal failed on a cancelled context", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		le := New(client, "node1", zap.NewNop().Sugar())
		le.tryBecomeLeader(context.Background())
		require.True(t, le.IsLeader())

		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		le.tryBecomeLeader(cancelled)
		assert.True(t, le.IsLeader(), "a cancelled renewal made the node forget its leadership")

		le.releaseLeadership(context.Background())
		assert.False(t, mr.Exists(Key))
	})

	t.Run("release leadership when not leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := New(client, "node1", logger)

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
		le1 := New(client, "node1", logger)
		ctx := context.Background()
		le1.tryBecomeLeader(ctx)
		assert.True(t, le1.IsLeader())

		// Node 2 tries to release (but it's not the leader)
		le2 := New(client, "node2", logger)
		le2.setLeaderStatus(true) // Force it to think it's leader
		le2.releaseLeadership(ctx)

		// Verify node1 is still the leader
		value, err := client.Get(ctx, Key).Result()
		require.NoError(t, err)
		assert.Equal(t, "node1", value)
	})
}

func TestElector_WaitForLeadership(t *testing.T) {
	t.Run("returns immediately when already leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := New(client, "node1", logger)

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
		le := New(client, "node1", logger)

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
		le := New(client, "node1", logger)

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
func newElection(client *redis.Client, nodeID string) *Elector {
	return New(client, nodeID, zap.NewNop().Sugar(), WithRenewalInterval(2*time.Millisecond))
}

func leaderHolder(t *testing.T, client *redis.Client) string {
	t.Helper()
	holder, err := client.Get(context.Background(), Key).Result()
	require.NoError(t, err)
	return holder
}

func TestElector_StartStop(t *testing.T) {
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
		assert.False(t, mr.Exists(Key))
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
		require.NoError(t, client.Set(ctx, Key, "node-crashed", TTL).Err())
		le := newElection(client, "node1")

		le.Start(ctx)
		defer le.Stop()
		assert.Never(t, le.IsLeader, 20*time.Millisecond, time.Millisecond)

		mr.FastForward(TTL)

		require.Eventually(t, le.IsLeader, time.Second, time.Millisecond)
		assert.Equal(t, "node1", leaderHolder(t, client))
	})
}

func TestElector_SetLeaderStatus(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()

	logger := zap.NewNop().Sugar()
	le := New(client, "node1", logger)

	assert.False(t, le.IsLeader())

	le.setLeaderStatus(true)
	assert.True(t, le.IsLeader())

	le.setLeaderStatus(false)
	assert.False(t, le.IsLeader())
}

// A term of leadership starts when the node becomes the leader, and lasts through the renewals
func TestElector_LeadershipTerm(t *testing.T) {
	le := New(nil, "node1", zap.NewNop().Sugar())

	leads, term := le.leadership()
	assert.False(t, leads)
	assert.Zero(t, term, "a node that never led is in no term")

	le.setLeaderStatus(true)
	leads, first := le.leadership()
	assert.True(t, leads)
	assert.NotZero(t, first)

	le.setLeaderStatus(true) // a renewal
	leads, term = le.leadership()
	assert.True(t, leads)
	assert.Equal(t, first, term, "a renewal started a new term")

	le.setLeaderStatus(false)
	leads, term = le.leadership()
	assert.False(t, leads)
	assert.Equal(t, first, term, "a follower is still in the term it lost")
	le.setLeaderStatus(false)
	_, term = le.leadership()
	assert.Equal(t, first, term)

	le.setLeaderStatus(true)
	leads, term = le.leadership()
	assert.True(t, leads)
	assert.Greater(t, term, first, "the node led again in the same term")
}

// The api reads IsLeader from the goroutines of the jobs it runs, while the election renews the
// lock. Run with -race, this fails when the status is a plain bool.
func TestElector_IsLeaderMayBeReadWhileTheElectionRuns(t *testing.T) {
	ctx := context.Background()
	client, mr := setupTestRedis(t)
	defer mr.Close()
	le := newElection(client, "node1")
	le.Start(ctx)
	defer le.Stop()

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = le.IsLeader()
				}
			}
		}()
	}

	// The status changes under the readers: node2 takes the lock, and node1 takes it back
	require.Eventually(t, le.IsLeader, time.Second, time.Millisecond)
	require.NoError(t, client.Set(ctx, Key, "node2", TTL).Err())
	require.Eventually(t, func() bool { return !le.IsLeader() }, time.Second, time.Millisecond)
	require.NoError(t, client.Del(ctx, Key).Err())
	require.Eventually(t, le.IsLeader, time.Second, time.Millisecond)

	close(stop)
	readers.Wait()
}

func TestElector_StopMayBeCalledAgain(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()
	le := newElection(client, "node1")
	le.Start(context.Background())
	require.Eventually(t, le.IsLeader, time.Second, time.Millisecond)

	le.Stop()
	le.Stop()

	assert.False(t, mr.Exists(Key))
}

func TestElector_DoneClosesWhenTheContextGivenToStartIsDone(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()
	le := newElection(client, "node1")
	ctx, cancel := context.WithCancel(context.Background())
	le.Start(ctx)
	require.Eventually(t, le.IsLeader, time.Second, time.Millisecond)

	cancel()

	select {
	case <-le.Done():
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the election didn't stop")
	}
	assert.False(t, mr.Exists(Key), "the lock is released")
}

// During a rolling upgrade, the producers of the previous version and the processes of this one
// contest one lock: it has to keep the key, the TTL and the renewal the producer had.
func TestElector_KeepsTheLockThePreviousVersionUsed(t *testing.T) {
	assert.Equal(t, "peekaping:producer:leader", Key)
	assert.Equal(t, 10*time.Second, TTL)
	assert.Equal(t, 5*time.Second, RenewalInterval)
}

// A leader that renews its lock only after the lock expires loses it between
// renewals, and another node takes over. A zero interval makes the
// renewal ticker panic when the leader starts.
func TestLeaderRenewsBeforeLeaseExpires(t *testing.T) {
	assert.Greater(t, RenewalInterval, time.Duration(0))
	assert.Less(t, RenewalInterval, TTL)
}

func TestElector_TheLockExpiresAfterTheTTL(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()
	le := New(client, "node1", zap.NewNop().Sugar())

	le.tryBecomeLeader(context.Background())

	assert.Equal(t, 10*time.Second, mr.TTL(Key))
}

func TestNodeID(t *testing.T) {
	host, err := os.Hostname()
	require.NoError(t, err)

	assert.Equal(t, fmt.Sprintf("%s-%d", host, os.Getpid()), NodeID())
}
