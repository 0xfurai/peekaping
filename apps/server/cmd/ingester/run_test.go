package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/shared"
	graceful "peekaping/internal/shutdown"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shutdownOrder is what a shutdown that stops everything in time logs, in this order
var shutdownOrder = []string{
	"Shutdown signal received, shutting down",
	"Stopped the ingester",
	"Closed the event bus",
	"SQLite database closed gracefully",
	"Closed the database",
	"Ingester shut down",
}

const monitorID = "monitor-1"

// enqueueIngest queues the result of a check of the monitor for the ingester, as the worker does
func enqueueIngest(t *testing.T, q *queues) {
	t.Helper()
	now := time.Now().UTC()
	payload, err := json.Marshal(ingester.Result{
		MonitorID:       monitorID,
		MonitorName:     "Monitor " + monitorID,
		MonitorType:     "push",
		MonitorInterval: 60,
		MonitorTimeout:  16,
		Status:          shared.MonitorStatusUp,
		Message:         "OK",
		PingMs:          12,
		StartTime:       now,
		EndTime:         now,
	})
	require.NoError(t, err)
	_, err = q.client.Enqueue(asynq.NewTask(ingester.TaskTypeIngester, payload), asynq.Queue("ingester"))
	require.NoError(t, err)
}

// waitUntilActive waits until the ingester has taken the task, and gives its handler a moment to
// reach its write, which waits for the database lock the test holds
func waitUntilActive(t *testing.T, q *queues) {
	t.Helper()
	require.Eventually(t, func() bool {
		tasks, err := q.inspector.ListActiveTasks("ingester")
		return err == nil && len(tasks) == 1
	}, wait, 10*time.Millisecond, "the ingester didn't take the task")
	time.Sleep(500 * time.Millisecond)
}

func TestRun_LetsTheIngestInFlightFinishStoringItsHeartbeatBeforeStopping(t *testing.T) {
	cfg, redis := loadConfig(t, nil)
	migrateDatabase(t, cfg)
	insertMonitor(t, cfg, monitorID)
	q := newQueues(t, redis)
	logger, logs := observedLogger()
	in := startIngester(t, cfg, logger, logs)
	// Another writer holds the database, so the ingest can't store its heartbeat yet
	unlock := lockDatabase(t, cfg.DBName)
	enqueueIngest(t, q)
	waitUntilActive(t, q)

	startedAt := time.Now()
	stopped := make(chan error, 1)
	go func() { stopped <- in.stop(t) }()
	time.Sleep(time.Second)
	select {
	case err := <-stopped:
		require.FailNow(t, "run returned with an ingest in flight", "%v", err)
	default:
	}
	assert.Zero(t, logs.FilterMessage("Stopped the ingester").Len(), "the ingester waits for the ingest in flight")
	unlock()

	select {
	case err := <-stopped:
		require.NoError(t, err, "run returns nil, so the ingester exits 0")
	case <-time.After(shutdownTimeouts.Total()):
		require.FailNow(t, "run didn't return within the shutdown deadline")
	}
	t.Logf("shut down in %s", time.Since(startedAt).Round(time.Millisecond))

	assert.Equal(t, 1, storedHeartbeats(t, cfg, monitorID), "the ingest in flight stored its heartbeat")
	info, err := q.inspector.GetQueueInfo("ingester")
	require.NoError(t, err)
	assert.Zero(t, info.Active+info.Pending+info.Retry, "the ingest isn't left in the queue")
	assert.Equal(t, shutdownOrder, logged(logs, shutdownOrder), "the shutdown's steps, in the order logged")
}

func TestRun_ReturnsAnErrorWhenTheDatabaseCantBeOpened(t *testing.T) {
	// SQLite can't create its file in a directory that doesn't exist
	cfg, _ := loadConfig(t, map[string]string{"DB_NAME": filepath.Join(t.TempDir(), "missing", "peekaping.db")})
	logger, logs := observedLogger()

	err := run(context.Background(), cfg, logger)

	require.Error(t, err, "the ingester exits 1")
	assert.ErrorContains(t, err, "failed to build the ingester")
	assert.ErrorContains(t, err, "failed to ping database")
	assert.Zero(t, logs.FilterMessage("Ingester shut down").Len())
}

func TestServe_StopsWhatItStartedAndReturnsAnErrorWhenStartingFails(t *testing.T) {
	cfg, _ := loadConfig(t, nil)
	migrateDatabase(t, cfg)
	logger, logs := observedLogger()
	p, err := build(cfg, logger)
	require.NoError(t, err)
	// The ingester is started already, so that serve's Start fails. What had started is this
	// ingester, the stats handlers on the event bus and the database, which serve stops.
	require.NoError(t, p.Ingester.Start())

	err = serve(context.Background(), p, cfg.DBType, logger)

	require.Error(t, err, "the ingester exits 1")
	assert.ErrorContains(t, err, "failed to start the ingester")
	assert.ErrorContains(t, err, "the server is already running")
	require.Len(t, logs.FilterMessage("Failed to start the ingester, shutting down").All(), 1)
	order := []string{
		"Failed to start the ingester, shutting down",
		"Stopped the ingester",
		"Closed the event bus",
		"SQLite database closed gracefully",
		"Closed the database",
	}
	assert.Equal(t, order, logged(logs, order), "what had started stopped, in order")
	assert.Zero(t, logs.FilterMessage("Ingester shut down").Len())
}

func TestRun_FailsAndLeavesTheDatabaseOpenWhenAnIngestOutlastsTheJobsShare(t *testing.T) {
	// The ingest waits for the database lock past the jobs' share: asynq gives up on it before
	// the share ends and cancels it, but its write goes on waiting
	defer func(saved graceful.Timeouts) { shutdownTimeouts = saved }(shutdownTimeouts)
	shutdownTimeouts.Jobs = 3 * time.Second
	cfg, redis := loadConfig(t, nil)
	migrateDatabase(t, cfg)
	insertMonitor(t, cfg, monitorID)
	q := newQueues(t, redis)
	logger, logs := observedLogger()
	in := startIngester(t, cfg, logger, logs)
	unlock := lockDatabase(t, cfg.DBName)
	enqueueIngest(t, q)
	waitUntilActive(t, q)

	err := in.stop(t)

	require.Error(t, err, "the ingester exits 1")
	assert.ErrorContains(t, err, "the ingester didn't stop in time")
	left := logs.FilterMessage("Left the database open, since some of what uses it is still running").All()
	require.Len(t, left, 1)
	assert.Equal(t, []any{"the ingester"}, left[0].ContextMap()["running"])
	assert.Zero(t, logs.FilterMessage("Performing graceful SQLite shutdown...").Len(), "the database isn't closed under the ingest")
	assert.Zero(t, logs.FilterMessage("Closed the database").Len())
	assert.Equal(t, 1, logs.FilterMessage("Closed the event bus").Len(), "the event bus still closes, in its own share")
	unlock()
}
