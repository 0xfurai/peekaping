package leader

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const lookEvery = 2 * time.Millisecond

// follower is a node whose election runs every few milliseconds, with the logs it writes
type follower struct {
	*Elector
	client *redis.Client
	mr     *miniredis.Miniredis
	logs   *observer.ObservedLogs
}

// startElection runs node1's election until the test ends. node2 holds the lock when it holds is
// true, until the test deletes it or lets it expire.
func startElection(t *testing.T, node2Holds bool) *follower {
	t.Helper()
	client, mr := setupTestRedis(t)
	t.Cleanup(mr.Close)
	core, logs := observer.New(zap.InfoLevel)
	if node2Holds {
		require.NoError(t, client.Set(context.Background(), Key, "node2", TTL).Err())
	}
	e := New(client, "node1", zap.New(core).Sugar(), WithRenewalInterval(lookEvery))
	e.Start(context.Background())
	t.Cleanup(e.Stop)
	return &follower{Elector: e, client: client, mr: mr, logs: logs}
}

// node2 takes the lock from node1, which loses the leadership at its next renewal
func (f *follower) node2Takes(t *testing.T) {
	t.Helper()
	require.NoError(t, f.client.Set(context.Background(), Key, "node2", TTL).Err())
	require.Eventually(t, func() bool { return !f.IsLeader() }, time.Second, time.Millisecond)
}

// node2Leaves is node2 releasing the lock, which node1 takes at its next renewal
func (f *follower) node2Leaves(t *testing.T) {
	t.Helper()
	require.NoError(t, f.client.Del(context.Background(), Key).Err())
	require.Eventually(t, f.IsLeader, time.Second, time.Millisecond)
}

// job is a function that RunWhileLeader runs, and what it saw
type job struct {
	started atomic.Int32 // how many times it was started
	running atomic.Int32 // how many run now
	overlap atomic.Bool  // whether two ever ran at once
	ended   atomic.Int32 // how many times its context was done
	stopped atomic.Int32 // how many times it returned, after its context was done
	// windDown is how long it takes to return once its context is done
	windDown time.Duration
}

func (j *job) run(ctx context.Context) error {
	j.started.Add(1)
	if j.running.Add(1) > 1 {
		j.overlap.Store(true)
	}
	<-ctx.Done()
	j.ended.Add(1)
	time.Sleep(j.windDown)
	j.running.Add(-1)
	j.stopped.Add(1)
	return nil
}

// runWhileLeader runs RunWhileLeader on the job, and returns what ends it and a channel that
// closes once it has returned
func runWhileLeader(f *follower, fn func(context.Context) error) (stop context.CancelFunc, returned <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.RunWhileLeader(ctx, "the job", lookEvery, fn)
	}()
	return cancel, done
}

func waitReturned(t *testing.T, returned <-chan struct{}) {
	t.Helper()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "RunWhileLeader didn't return")
	}
}

func TestRunWhileLeader_RunsTheJobWhileTheNodeLeads(t *testing.T) {
	f := startElection(t, false)
	j := &job{}

	stop, returned := runWhileLeader(f, j.run)
	defer stop()

	require.Eventually(t, func() bool { return j.running.Load() == 1 }, time.Second, time.Millisecond, "the leader never started the job")
	assert.Equal(t, int32(1), j.started.Load())
	assert.NotZero(t, f.logs.FilterMessage("Starting the job as leader").Len())

	stop()
	waitReturned(t, returned)
}

func TestRunWhileLeader_DoesNotRunTheJobOnAFollower(t *testing.T) {
	f := startElection(t, true)
	j := &job{}

	stop, returned := runWhileLeader(f, j.run)
	defer stop()

	assert.Never(t, func() bool { return j.started.Load() > 0 }, 50*time.Millisecond, time.Millisecond, "a follower ran the job")

	// node2 stops renewing: its lock runs out and this node takes over
	f.mr.FastForward(TTL)
	require.Eventually(t, func() bool { return j.running.Load() == 1 }, time.Second, time.Millisecond)

	stop()
	waitReturned(t, returned)
}

func TestRunWhileLeader_CancelsTheJobWhenTheNodeLosesTheLeadershipAndRunsItAgainWhenItGetsItBack(t *testing.T) {
	f := startElection(t, false)
	j := &job{}
	stop, returned := runWhileLeader(f, j.run)
	defer stop()
	require.Eventually(t, func() bool { return j.running.Load() == 1 }, time.Second, time.Millisecond)

	f.node2Takes(t)

	require.Eventually(t, func() bool { return j.stopped.Load() == 1 }, time.Second, time.Millisecond, "the job kept running without the leadership")
	assert.Zero(t, j.running.Load())
	assert.NotZero(t, f.logs.FilterMessage("Lost leadership, stopping the job").Len())

	f.node2Leaves(t)

	require.Eventually(t, func() bool { return j.running.Load() == 1 }, time.Second, time.Millisecond, "the job didn't run again")
	assert.Equal(t, int32(2), j.started.Load())
	assert.False(t, j.overlap.Load())

	stop()
	waitReturned(t, returned)
}

// A job that outlives its leadership could run beside the next leader's
func TestRunWhileLeader_NeverRunsTheJobTwiceAtOnce(t *testing.T) {
	f := startElection(t, false)
	j := &job{windDown: 30 * time.Millisecond}
	stop, returned := runWhileLeader(f, j.run)
	defer stop()
	require.Eventually(t, func() bool { return j.running.Load() == 1 }, time.Second, time.Millisecond)

	// The node gets the leadership back while the job is still winding down
	f.node2Takes(t)
	require.Eventually(t, func() bool { return j.ended.Load() == 1 }, time.Second, time.Millisecond)
	f.node2Leaves(t)

	require.Eventually(t, func() bool { return j.started.Load() == 2 }, time.Second, time.Millisecond, "the job didn't run again")
	assert.Equal(t, int32(1), j.stopped.Load(), "the job was started again before it returned")
	assert.False(t, j.overlap.Load(), "two runs of the job overlapped")

	stop()
	waitReturned(t, returned)
}

func TestRunWhileLeader_ReturnsOnceTheContextIsDoneAndTheJobHasReturned(t *testing.T) {
	f := startElection(t, false)
	j := &job{windDown: 30 * time.Millisecond}
	stop, returned := runWhileLeader(f, j.run)
	defer stop()
	require.Eventually(t, func() bool { return j.running.Load() == 1 }, time.Second, time.Millisecond)

	stop()
	waitReturned(t, returned)

	assert.Equal(t, int32(1), j.stopped.Load(), "RunWhileLeader returned while the job was still running")
	assert.NotZero(t, f.logs.FilterMessage("Context cancelled, stopping the job").Len())
}

func TestRunWhileLeader_ReturnsOnceTheContextIsDoneOnAFollower(t *testing.T) {
	f := startElection(t, true)
	stop, returned := runWhileLeader(f, new(job).run)

	stop()

	waitReturned(t, returned)
}

func TestRunWhileLeader_LogsAJobThatFailsAndRunsItAgainWhileTheNodeLeads(t *testing.T) {
	f := startElection(t, false)
	var started atomic.Int32
	stop, returned := runWhileLeader(f, func(ctx context.Context) error {
		if started.Add(1) < 3 {
			return errors.New("not ready")
		}
		<-ctx.Done()
		return nil
	})
	defer stop()

	require.Eventually(t, func() bool { return started.Load() == 3 }, time.Second, time.Millisecond, "the job wasn't run again")
	failures := f.logs.FilterMessage("Failed to run the job").All()
	require.Len(t, failures, 2)
	assert.Equal(t, "not ready", failures[0].ContextMap()["error"])
	assert.Never(t, func() bool { return started.Load() > 3 }, 30*time.Millisecond, time.Millisecond, "the job runs on, so it isn't run again")

	stop()
	waitReturned(t, returned)
}

func TestRunWhileLeader_DoesNotRunAJobThatIsDoneAgainUntilTheNodeHasLostTheLeadership(t *testing.T) {
	f := startElection(t, false)
	var started atomic.Int32
	stop, returned := runWhileLeader(f, func(context.Context) error {
		started.Add(1)
		return nil
	})
	defer stop()
	require.Eventually(t, func() bool { return started.Load() == 1 }, time.Second, time.Millisecond)

	assert.Never(t, func() bool { return started.Load() > 1 }, 50*time.Millisecond, time.Millisecond, "a job that is done ran again")

	f.node2Takes(t)
	f.node2Leaves(t)
	require.Eventually(t, func() bool { return started.Load() == 2 }, time.Second, time.Millisecond, "the job didn't run again for the new term")

	stop()
	waitReturned(t, returned)
}

// A job stops with its context's error when its caller stops: that is no failure
func TestRunWhileLeader_LogsNoErrorForAJobThatFailsBecauseTheContextIsDone(t *testing.T) {
	f := startElection(t, false)
	running := make(chan struct{})
	stop, returned := runWhileLeader(f, func(ctx context.Context) error {
		close(running)
		<-ctx.Done()
		return ctx.Err()
	})
	defer stop()
	<-running

	stop()
	waitReturned(t, returned)

	assert.Empty(t, f.logs.FilterLevelExact(zap.ErrorLevel).All())
}
