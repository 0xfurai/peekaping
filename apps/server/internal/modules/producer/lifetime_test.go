package producer

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/monitor"

	"github.com/alicebob/miniredis/v2/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// releasesLeadership is whether the Redis command is the election's release of the leadership,
// not another script of the producer's
func releasesLeadership(cmd string, args []string) bool {
	return strings.EqualFold(cmd, "EVAL") && slices.Contains(args, LeaderKey)
}

// holdLeadershipRelease makes Redis hold the command that releases the leadership until the
// returned function is called, which the test's cleanup does too
func holdLeadershipRelease(t *testing.T, e *leaderEngine) (release func()) {
	t.Helper()
	released := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(released) }) }
	e.mr.Server().SetPreHook(func(_ *server.Peer, cmd string, args ...string) bool {
		if releasesLeadership(cmd, args) {
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
	// The claim loop takes the monitor from the due set as soon as it is due, so it may be leased
	require.Eventually(t, func() bool {
		_, due := e.score(SchedDueKey, "mon-1")
		_, leased := e.score(SchedLeaseKey, "mon-1")
		return due || leased
	}, 5*time.Second, 2*time.Millisecond, "the producer never scheduled mon-1")
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

func TestStop_StopsSchedulingAndTheWatchdogBeforeItReleasesTheLeadership(t *testing.T) {
	e := startedLeader(t, context.Background())
	steps := []string{"Stopping producer", "Stopped scheduling", "Stopped the watchdog", "Producer stopped"}
	// The steps Stop has logged when Redis gets the command that releases the leadership
	var loggedAtRelease []string
	atRelease := make(chan struct{})
	releaseHeld := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(releaseHeld) }) })
	e.mr.Server().SetPreHook(func(_ *server.Peer, cmd string, args ...string) bool {
		if releasesLeadership(cmd, args) {
			for _, entry := range e.logs.All() {
				if slices.Contains(steps, entry.Message) {
					loggedAtRelease = append(loggedAtRelease, entry.Message)
				}
			}
			close(atRelease)
			<-releaseHeld
		}
		return false
	})

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		e.producer.Stop()
	}()

	select {
	case <-atRelease:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the leadership wasn't released")
	}
	assert.Equal(t, steps[:3], loggedAtRelease, "the leadership is released after scheduling and the watchdog stopped")
	once.Do(func() { close(releaseHeld) })
	<-stopped
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

// observe makes the producer log to the returned logs
func observe(p *Producer) *observer.ObservedLogs {
	core, logs := observer.New(zap.DebugLevel)
	p.logger = zap.New(core).Sugar()
	return logs
}

// stopWhenRedisGets ends the producer's lifetime as Redis gets the command, the first time. The
// command is answered, and the producer's next call finds its context done: it is what happens
// when Stop cancels a loop that is between two calls to Redis.
func stopWhenRedisGets(e *engine, command string) {
	var once sync.Once
	e.mr.Server().SetPreHook(func(_ *server.Peer, cmd string, _ ...string) bool {
		if strings.EqualFold(cmd, command) {
			once.Do(e.producer.cancel)
		}
		return false
	})
}

func TestRunProducer_LogsNoErrorWhenTheProducerStopsDuringAClaim(t *testing.T) {
	e := newEngine(t)
	logs := observe(e.producer)
	stopWhenRedisGets(e, "TIME") // the claim comes right after the time

	e.producer.wg.Add(1)
	err := e.producer.runProducer(0)

	assert.ErrorIs(t, err, context.Canceled, "the loop ends with its context's error, which Start takes for no failure")
	assert.Empty(t, logs.FilterLevelExact(zap.ErrorLevel).All())
}

func TestRunReclaimer_LogsNoErrorWhenTheProducerStopsDuringAReclaim(t *testing.T) {
	e := newEngine(t)
	logs := observe(e.producer)
	stopWhenRedisGets(e, "TIME") // the reclaim comes right after the time

	e.producer.wg.Add(1)
	go e.producer.runReclaimer()

	waitForGoroutines(t, &e.producer.wg)
	assert.Empty(t, logs.FilterLevelExact(zap.ErrorLevel).All())
}

func TestRunScheduleRefresher_LogsNoErrorWhenTheProducerStopsDuringARefresh(t *testing.T) {
	e := newEngine(t, &monitor.Model{ID: "mon-1", Active: true, Interval: 60})
	logs := observe(e.producer)
	e.producer.scheduleRefreshInterval = 5 * time.Millisecond
	stopWhenRedisGets(e, "TIME") // the schedule is written right after the time
	syncCtx, syncCancel := context.WithCancel(e.producer.ctx)
	defer syncCancel()

	e.producer.wg.Add(1)
	go e.producer.runScheduleRefresher(syncCtx)

	waitForGoroutines(t, &e.producer.wg)
	assert.Empty(t, logs.FilterLevelExact(zap.ErrorLevel).All())
}

func TestRunLeadershipMonitor_LogsNoErrorWhenTheProducerStopsWhileSyncingStarts(t *testing.T) {
	e := newLeaderEngine(t, &monitor.Model{ID: "mon-1", Active: true, Interval: 60})
	stopWhenRedisGets(e.engine, "ZRANGE") // the schedule is read in two calls

	e.start(t)

	waitForGoroutines(t, &e.producer.wg)
	assert.Empty(t, e.logs.FilterLevelExact(zap.ErrorLevel).All())
}

func TestRedisNowMs_DoesntWarnWhenTheProducerStopped(t *testing.T) {
	e := newEngine(t)
	logs := observe(e.producer)
	e.producer.cancel()

	nowMs := e.producer.redisNowMs()

	assert.WithinDuration(t, time.Now(), time.UnixMilli(nowMs), time.Minute, "the local time stands in")
	assert.Empty(t, logs.FilterLevelExact(zap.WarnLevel).All())
}
