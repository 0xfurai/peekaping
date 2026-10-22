package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/worker"
	graceful "peekaping/internal/shutdown"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// shutdownOrder is what a shutdown that stops everything in time logs, in this order
var shutdownOrder = []string{
	"Shutdown signal received, shutting down",
	"Stopped the worker",
	"Closed the event bus",
	"Worker shut down",
}

// slowTarget is a service a check runs against, which answers after delay. arrived gets a value
// each time a request reaches it.
func slowTarget(t *testing.T, delay time.Duration) (target *httptest.Server, arrived <-chan struct{}) {
	t.Helper()
	requests := make(chan struct{}, 8)
	target = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- struct{}{}
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(target.Close)
	return target, requests
}

// enqueueCheck queues a check of an HTTP monitor for the worker, as the producer does
func enqueueCheck(t *testing.T, q *queues, url string) {
	t.Helper()
	payload, err := json.Marshal(worker.HealthCheckTaskPayload{
		MonitorID:   "monitor-1",
		MonitorName: "Slow target",
		MonitorType: "http",
		Interval:    60,
		Timeout:     20,
		Config:      fmt.Sprintf(`{"url": %q, "method": "GET", "encoding": "json", "authMethod": "none", "accepted_statuscodes": ["2XX"]}`, url),
		ScheduledAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	_, err = q.client.Enqueue(asynq.NewTask(worker.TaskTypeHealthCheck, payload), asynq.Queue("healthcheck"))
	require.NoError(t, err)
}

func receive(t *testing.T, c <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(wait):
		require.FailNow(t, what)
	}
}

// ingestTasks are the results waiting for the ingester
func ingestTasks(t *testing.T, q *queues) []*asynq.TaskInfo {
	t.Helper()
	tasks, err := q.inspector.ListPendingTasks("ingester")
	if errors.Is(err, asynq.ErrQueueNotFound) {
		return nil // nothing was ever enqueued
	}
	require.NoError(t, err)
	return tasks
}

func TestRun_LetsTheCheckInFlightFinishAndEnqueueItsResultBeforeStopping(t *testing.T) {
	cfg, redis := loadConfig(t, nil)
	q := newQueues(t, redis)
	logger, logs := observedLogger()
	target, arrived := slowTarget(t, 2*time.Second)
	in := startWorker(t, cfg, logger)
	enqueueCheck(t, q, target.URL)
	receive(t, arrived, "the worker didn't run the check")

	startedAt := time.Now()
	err := in.stop(t)
	took := time.Since(startedAt)
	t.Logf("shut down in %s", took.Round(time.Millisecond))

	require.NoError(t, err, "run returns nil, so the worker exits 0")
	assert.Greater(t, took, time.Second, "the shutdown waited for the check")
	tasks := ingestTasks(t, q)
	require.Len(t, tasks, 1, "the check's result is enqueued for the ingester")
	assert.Equal(t, ingester.TaskTypeIngester, tasks[0].Type)
	var result ingester.Result
	require.NoError(t, json.Unmarshal(tasks[0].Payload, &result))
	assert.Equal(t, "monitor-1", result.MonitorID)
	assert.Equal(t, shared.MonitorStatusUp, result.Status, "the check finished, not cut short")
	info, err := q.inspector.GetQueueInfo("healthcheck")
	require.NoError(t, err)
	assert.Zero(t, info.Active+info.Pending+info.Retry, "the check isn't left in the queue")
	assert.Equal(t, shutdownOrder, logged(logs, shutdownOrder), "the shutdown's steps, in the order logged")
	assert.Empty(t, logs.FilterLevelExact(zap.ErrorLevel).All(), "a clean shutdown logs no error")
}

func TestRun_GivesUpOnACheckThatOutlastsTheJobsShareAndPutsItBackInTheQueue(t *testing.T) {
	// asynq gives up on the check before the share is over, so that the shutdown doesn't run out
	// of time on it
	defer func(saved graceful.Timeouts) { shutdownTimeouts = saved }(shutdownTimeouts)
	shutdownTimeouts.Jobs = 5 * time.Second
	cfg, redis := loadConfig(t, nil)
	q := newQueues(t, redis)
	logger, logs := observedLogger()
	target, arrived := slowTarget(t, time.Minute)
	in := startWorker(t, cfg, logger)
	enqueueCheck(t, q, target.URL)
	receive(t, arrived, "the worker didn't run the check")

	startedAt := time.Now()
	err := in.stop(t)
	took := time.Since(startedAt)
	t.Logf("shut down in %s", took.Round(time.Millisecond))

	require.NoError(t, err, "everything stopped in its share")
	assert.Less(t, took, shutdownTimeouts.Jobs, "asynq gave up before the share ended")
	assert.Empty(t, ingestTasks(t, q), "a check that was cut short says nothing about its target")
	info, err := q.inspector.GetQueueInfo("healthcheck")
	require.NoError(t, err)
	assert.Equal(t, 1, info.Pending, "the check is back in the queue, for another worker")
	assert.Equal(t, shutdownOrder, logged(logs, shutdownOrder), "the shutdown's steps, in the order logged")
}

func TestServe_StopsWhatItStartedAndReturnsAnErrorWhenStartingFails(t *testing.T) {
	cfg, _ := loadConfig(t, nil)
	logger, logs := observedLogger()
	p, err := build(cfg, logger)
	require.NoError(t, err)
	// The worker is started already, so that serve's Start fails. What had started is this
	// worker and the event bus, which serve stops.
	require.NoError(t, p.Worker.Start())

	err = serve(context.Background(), p, logger)

	require.Error(t, err, "the worker exits 1")
	assert.ErrorContains(t, err, "failed to start the worker")
	assert.ErrorContains(t, err, "the server is already running")
	require.Len(t, logs.FilterMessage("Failed to start the worker, shutting down").All(), 1)
	order := []string{
		"Failed to start the worker, shutting down",
		"Stopped the worker",
		"Closed the event bus",
	}
	assert.Equal(t, order, logged(logs, order), "what had started stopped, in order")
	assert.Zero(t, logs.FilterMessage("Worker shut down").Len())
}
