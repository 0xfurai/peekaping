package producer

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/monitor"

	"github.com/alicebob/miniredis/v2/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// holdLeadershipRelease makes Redis hold the command that releases the leadership until the
// returned function is called, which the test's cleanup does too
func holdLeadershipRelease(t *testing.T, e *leaderEngine) (release func()) {
	t.Helper()
	released := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(released) }) }
	e.mr.Server().SetPreHook(func(_ *server.Peer, cmd string, _ ...string) bool {
		if strings.EqualFold(cmd, "EVAL") {
			<-released
		}
		return false
	})
	t.Cleanup(release)
	return release
}

// leaderKeyExists is whether any node holds the leadership
func leaderKeyExists(t *testing.T, e *leaderEngine) bool {
	t.Helper()
	exists, err := e.rdb.Exists(context.Background(), LeaderKey).Result()
	require.NoError(t, err)
	return exists == 1
}

// startedLeader starts the producer with the context, and returns once it is the leader and
// syncing the schedule
func startedLeader(t *testing.T, ctx context.Context) *leaderEngine {
	t.Helper()
	e := newLeaderEngine(t, &monitor.Model{ID: "mon-1", Active: true, Interval: 60})
	require.NoError(t, e.producer.Start(ctx))
	t.Cleanup(e.producer.Stop)
	e.eventuallyDueAt(t, "mon-1", t0)
	require.True(t, leaderKeyExists(t, e), "the producer isn't the leader")
	return e
}

func TestStart_StopsTheProducerAndReleasesLeadershipWhenItsContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := startedLeader(t, ctx)

	cancel()

	waitForGoroutines(t, &e.producer.wg)
	select {
	case <-e.producer.electionDone:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the election didn't stop")
	}
	assert.False(t, leaderKeyExists(t, e), "the leader key is still there")
}

func TestStop_ReturnsOnceTheLeadershipIsReleased(t *testing.T) {
	e := startedLeader(t, context.Background())
	release := holdLeadershipRelease(t, e)

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		e.producer.Stop()
	}()

	select {
	case <-stopped:
		require.FailNow(t, "Stop returned before the leadership was released")
	case <-time.After(300 * time.Millisecond):
	}
	release()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Stop didn't return once the leadership was released")
	}
	assert.False(t, leaderKeyExists(t, e), "the leader key is still there")
}

func TestStart_FailsWhenTheProducerAlreadyStarted(t *testing.T) {
	e := startedLeader(t, context.Background())

	err := e.producer.Start(context.Background())

	assert.EqualError(t, err, "producer already started")
}

func TestStop_DoesNothingBeforeStartAndMayBeCalledAgain(t *testing.T) {
	e := newLeaderEngine(t)

	e.producer.Stop()
	require.NoError(t, e.producer.Start(context.Background()))
	e.producer.Stop()
	e.producer.Stop()

	waitForGoroutines(t, &e.producer.wg)
}
