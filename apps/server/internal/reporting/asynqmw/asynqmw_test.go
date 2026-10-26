package asynqmw_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"peekaping/internal/reporting"
	"peekaping/internal/reporting/asynqmw"
	"peekaping/internal/reporting/reportingtest"

	"github.com/alicebob/miniredis/v2"
	"github.com/getsentry/sentry-go"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// functions are the names of the frames' functions
func functions(frames []sentry.Frame) []string {
	names := make([]string, 0, len(frames))
	for _, f := range frames {
		names = append(names, f.Function)
	}
	return names
}

//go:noinline
func panickingTask(context.Context, *asynq.Task) error {
	panic("task failed for https://secret.example.com/health?token=abc")
}

func failingTask(context.Context, *asynq.Task) error {
	return errors.New("check failed: connection refused")
}

// asynqRun runs one task through a real asynq server on miniredis, with the middleware on its
// mux, and returns the error the server's ErrorHandler got for it
func asynqRun(t *testing.T, handler asynq.HandlerFunc) error {
	t.Helper()
	redis := miniredis.RunT(t)
	redisOpt := asynq.RedisClientOpt{Addr: redis.Addr()}

	var mu sync.Mutex
	var failure error
	handled := make(chan struct{}, 1)
	server := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: 1,
		Queues:      map[string]int{"default": 1},
		ErrorHandler: asynq.ErrorHandlerFunc(func(_ context.Context, _ *asynq.Task, err error) {
			mu.Lock()
			failure = err
			mu.Unlock()
			handled <- struct{}{}
		}),
	})
	mux := asynq.NewServeMux()
	mux.Use(asynqmw.ReportPanics)
	mux.HandleFunc("test:task", handler)
	require.NoError(t, server.Start(mux))
	t.Cleanup(server.Shutdown)

	client := asynq.NewClient(redisOpt)
	t.Cleanup(func() { _ = client.Close() })
	_, err := client.Enqueue(asynq.NewTask("test:task", nil), asynq.MaxRetry(0))
	require.NoError(t, err)

	select {
	case <-handled:
	case <-time.After(10 * time.Second):
		t.Fatal("the task did not fail")
	}
	mu.Lock()
	defer mu.Unlock()
	return failure
}

// asynq turns a panic into an error that holds a message and no stack, so the ErrorHandler can't
// report it with a stack trace. The middleware sits inside asynq's recover instead.
func TestReportPanics_ReportsAPanicWithItsStackAndLeavesAsynqToHandleIt(t *testing.T) {
	recorder := reportingtest.Install(t)

	err := asynqRun(t, panickingTask)

	assert.True(t, asynq.IsPanicError(err), "asynq still recovers the panic and fails the task as before: %v", err)
	events := recorder.Events()
	require.Len(t, events, 1)
	event := events[0]
	assert.Equal(t, "asynq", event.Tags["component"])
	assert.Equal(t, "string", event.Exception[0].Type)
	assert.Contains(t, functions(event.Exception[0].Stacktrace.Frames), "panickingTask")
	assert.Equal(t, "panickingTask", event.Exception[0].Stacktrace.Frames[len(event.Exception[0].Stacktrace.Frames)-1].Function)
}

func TestReportPanics_ReportsNothingForATaskThatReturnsAnError(t *testing.T) {
	recorder := reportingtest.Install(t)

	err := asynqRun(t, failingTask)

	assert.False(t, asynq.IsPanicError(err))
	assert.Empty(t, recorder.Events(), "a failed check or a task error is not a panic")
}

func TestReportPanics_ReportsNothingWhileReportingIsOff(t *testing.T) {
	recorder := reportingtest.Install(t)
	reporting.SetEnabled(false)

	err := asynqRun(t, panickingTask)

	assert.True(t, asynq.IsPanicError(err))
	assert.Empty(t, recorder.Events())
}
