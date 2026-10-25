package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/leader"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// replicaJobs is the leader jobs of one api replica over the shared Redis, with a job that counts
// how long it runs: running is 1 while the replica's watchdog is up
type replicaJobs struct {
	*LeaderJobs
	name    string
	running atomic.Int32
	// starts is how many times the job was started
	starts atomic.Int32
	// finishDelay is how long the job takes to return once its context is done
	finishDelay time.Duration
}

// startReplica starts the leader jobs of the replica called name on the Redis at addr, checking
// the lock every few milliseconds, and stops them when the test ends
func startReplica(t *testing.T, addr, name string, finishDelay time.Duration) *replicaJobs {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = client.Close() })
	r := &replicaJobs{name: name, finishDelay: finishDelay}
	elector := leader.New(client, name, zap.NewNop().Sugar(), leader.WithRenewalInterval(5*time.Millisecond))
	r.LeaderJobs = NewLeaderJobs(elector, []LeaderJob{{
		Name: "the stale-monitor watchdog",
		Run: func(ctx context.Context) {
			r.starts.Add(1)
			r.running.Add(1)
			defer r.running.Add(-1)
			<-ctx.Done()
			time.Sleep(r.finishDelay)
		},
	}}, WithLeaderCheckEvery(2*time.Millisecond))
	r.Start()
	t.Cleanup(r.Stop)
	return r
}

// leaderOf is the replica among the two whose job runs, once exactly one does
func leaderOf(t *testing.T, a, b *replicaJobs) (leading, following *replicaJobs) {
	t.Helper()
	require.Eventually(t, func() bool { return a.running.Load()+b.running.Load() == 1 }, 2*time.Second, time.Millisecond)
	if a.running.Load() == 1 {
		return a, b
	}
	return b, a
}

// AC2: the watchdog and the probe sweep run on the one api replica that holds the leader lock
func TestLeaderJobs_RunOnTheReplicaThatHoldsTheLeaderLockOnly(t *testing.T) {
	server := miniredis.RunT(t)
	a := startReplica(t, server.Addr(), "replica-a", 0)
	b := startReplica(t, server.Addr(), "replica-b", 0)

	leading, following := leaderOf(t, a, b)

	// Long enough for the follower to have looked at the lock many times
	time.Sleep(100 * time.Millisecond)
	assert.EqualValues(t, 1, leading.running.Load())
	assert.Zero(t, following.running.Load(), "the follower runs none of the leader's jobs")
	assert.Zero(t, following.starts.Load())
	holder, err := server.Get(leader.Key)
	require.NoError(t, err)
	assert.Equal(t, leading.name, holder, "the lock the producer used to hold, under the same key")
}

// AC2: when the leader stops, another replica takes over
func TestLeaderJobs_AnotherReplicaTakesOverWhenTheLeaderStops(t *testing.T) {
	server := miniredis.RunT(t)
	a := startReplica(t, server.Addr(), "replica-a", 0)
	b := startReplica(t, server.Addr(), "replica-b", 0)
	leading, following := leaderOf(t, a, b)

	leading.Stop()

	require.Eventually(t, func() bool { return following.running.Load() == 1 }, 2*time.Second, time.Millisecond,
		"the other replica runs the jobs once the leader released the lock")
	assert.Zero(t, leading.running.Load())
}

// AC2: a leader that died without releasing the lock is replaced once the lock's TTL has run out
func TestLeaderJobs_AFollowerTakesOverWithinTheLockTTLWhenTheLeaderDies(t *testing.T) {
	server := miniredis.RunT(t)
	// A leader that died: its lock is there, with the TTL it was last renewed with
	require.NoError(t, server.Set(leader.Key, "dead-replica"))
	server.SetTTL(leader.Key, leader.TTL)
	follower := startReplica(t, server.Addr(), "replica-b", 0)

	time.Sleep(50 * time.Millisecond)
	assert.Zero(t, follower.running.Load(), "the lock of the dead leader keeps the follower out until it expires")

	server.FastForward(leader.TTL)

	require.Eventually(t, func() bool { return follower.running.Load() == 1 }, 2*time.Second, time.Millisecond)
}

// The leadership is released only once the jobs have returned: a watchdog that outlived its
// leadership could run beside the next leader's
func TestLeaderJobs_StopReleasesTheLeadershipOnlyAfterTheJobsReturned(t *testing.T) {
	server := miniredis.RunT(t)
	r := startReplica(t, server.Addr(), "replica-a", 150*time.Millisecond)
	require.Eventually(t, func() bool { return r.running.Load() == 1 }, 2*time.Second, time.Millisecond)

	// Looks at the lock and then at the job: a job that still runs after the lock is gone has
	// outlived its leadership
	var outlived atomic.Bool
	sampling, stopSampling := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(sampling)
		for {
			select {
			case <-stopSampling:
				return
			default:
			}
			if !server.Exists(leader.Key) && r.running.Load() > 0 {
				outlived.Store(true)
			}
			time.Sleep(time.Millisecond)
		}
	}()

	r.Stop()
	close(stopSampling)
	<-sampling

	assert.False(t, outlived.Load(), "the lock was released while the job still ran")
	assert.False(t, server.Exists(leader.Key), "the lock is released when Stop returns")
	assert.Zero(t, r.running.Load())
}
