package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"peekaping/internal/leader"
	graceful "peekaping/internal/shutdown"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// producerStopOrder is what stopping the producer logs, in this order: it stops scheduling and
// its watchdog, and only then releases the leadership
var producerStopOrder = []string{
	"Stopping producer",
	"Stopped scheduling",
	"Stopped the watchdog",
	"Released leadership",
	"Producer stopped",
}

// shutdownOrder is what a shutdown that stops everything in time logs, in this order
var shutdownOrder = slices.Concat(
	[]string{"Shutdown signal received, shutting down"},
	producerStopOrder,
	[]string{
		"Stopped the producer",
		"Closed the event bus",
		"SQLite database closed gracefully",
		"Closed the database",
		"Producer shut down",
	},
)

// waitUntilScheduling waits until the producer is the leader and has enqueued the check of the
// monitor for the worker, so that the shutdown finds it at work
func waitUntilScheduling(t *testing.T, redis *miniredis.Miniredis) {
	t.Helper()
	queues := inspector(t, redis)
	require.Eventually(t, func() bool {
		tasks, err := queues.ListPendingTasks("healthcheck")
		return redis.Exists(leader.Key) && err == nil && len(tasks) == 1
	}, wait, 10*time.Millisecond, "the producer didn't become the leader and enqueue a check")
}

func TestRun_StopsSchedulingReleasesLeadershipAndClosesTheDatabaseInOrder(t *testing.T) {
	cfg, redis := loadConfig(t, nil)
	migrateDatabase(t, cfg)
	insertMonitor(t, cfg, "monitor-1")
	logger, logs := observedLogger()
	in := startProducer(t, cfg, logger)
	waitUntilScheduling(t, redis)

	startedAt := time.Now()
	err := in.stop(t)
	t.Logf("shut down in %s", time.Since(startedAt).Round(time.Millisecond))

	require.NoError(t, err, "run returns nil, so the producer exits 0")
	assert.False(t, redis.Exists(leader.Key), "the leadership is released")
	assert.Equal(t, shutdownOrder, logged(logs, shutdownOrder), "the shutdown's steps, in the order logged")
	assert.Empty(t, logs.FilterLevelExact(zap.ErrorLevel).All(), "a clean shutdown logs no error")
}

func TestRun_ReturnsAnErrorWhenTheDatabaseCantBeOpened(t *testing.T) {
	// SQLite can't create its file in a directory that doesn't exist
	cfg, _ := loadConfig(t, map[string]string{"DB_NAME": filepath.Join(t.TempDir(), "missing", "peekaping.db")})
	logger, logs := observedLogger()

	err := run(context.Background(), cfg, logger)

	require.Error(t, err, "the producer exits 1")
	assert.ErrorContains(t, err, "failed to build the producer")
	assert.ErrorContains(t, err, "failed to ping database")
	assert.Zero(t, logs.FilterMessage("Producer shut down").Len())
}

func TestServe_StopsWhatItStartedAndReturnsAnErrorWhenStartingFails(t *testing.T) {
	cfg, redis := loadConfig(t, nil)
	migrateDatabase(t, cfg)
	logger, logs := observedLogger()
	p, err := build(cfg, logger)
	require.NoError(t, err)
	// The producer is started already, so that serve's Start fails. What had started is serve's
	// event listener, the event bus and the database, and this producer, which it stops.
	require.NoError(t, p.Producer.Start(context.Background()))
	require.Eventually(t, func() bool { return redis.Exists(leader.Key) }, wait, 10*time.Millisecond)

	err = serve(context.Background(), p, cfg.DBType, logger)

	require.Error(t, err, "the producer exits 1")
	assert.ErrorContains(t, err, "failed to start the producer")
	assert.ErrorContains(t, err, "producer already started")
	failed := logs.FilterMessage("Failed to start the producer, shutting down").All()
	require.Len(t, failed, 1)
	order := slices.Concat(
		[]string{"Failed to start the producer, shutting down"},
		producerStopOrder,
		[]string{
			"Stopped the producer",
			"Closed the event bus",
			"SQLite database closed gracefully",
			"Closed the database",
		},
	)
	assert.Equal(t, order, logged(logs, order), "what had started stopped, in order")
	assert.False(t, redis.Exists(leader.Key), "the leadership is released")
	assert.Zero(t, logs.FilterMessage("Producer shut down").Len())
}

func TestRun_FailsAndLeavesTheDatabaseOpenWhenTheProducerDoesntStopInTime(t *testing.T) {
	// Redis holds the command that releases the leadership past the producer's share, so the
	// election, and so the producer, are still running when it ends
	defer func(saved graceful.Timeouts) { shutdownTimeouts = saved }(shutdownTimeouts)
	shutdownTimeouts.Jobs = 500 * time.Millisecond
	cfg, redis := loadConfig(t, nil)
	migrateDatabase(t, cfg)
	logger, logs := observedLogger()
	in := startProducer(t, cfg, logger)
	require.Eventually(t, func() bool { return redis.Exists(leader.Key) }, wait, 10*time.Millisecond)
	released := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(released) }) }
	t.Cleanup(release)
	redis.Server().SetPreHook(func(_ *server.Peer, cmd string, _ ...string) bool {
		if strings.EqualFold(cmd, "EVAL") {
			<-released
		}
		return false
	})

	err := in.stop(t)

	require.Error(t, err, "the producer exits 1")
	assert.ErrorContains(t, err, "the producer didn't stop in time")
	left := logs.FilterMessage("Left the database open, since some of what uses it is still running").All()
	require.Len(t, left, 1)
	assert.Equal(t, []any{"the producer"}, left[0].ContextMap()["running"])
	assert.Zero(t, logs.FilterMessage("Performing graceful SQLite shutdown...").Len(), "the database isn't closed under the producer")
	assert.Zero(t, logs.FilterMessage("Closed the database").Len())
	assert.Equal(t, 1, logs.FilterMessage("Closed the event bus").Len(), "the event bus still closes, in its own share")

	// The release goes through once Redis answers, past the deadline
	release()
	require.Eventually(t, func() bool { return !redis.Exists(leader.Key) }, wait, 10*time.Millisecond)
}
