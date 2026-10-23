package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/ingester"
	"peekaping/internal/reporting"
	"peekaping/internal/reporting/reportingtest"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// blockingIngester holds the result of a check, ignoring its context, until released
type blockingIngester struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingIngester) Ingest(context.Context, *ingester.Result) error {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return nil
}

// Asynq gives up on a task still running after its ShutdownTimeout: it puts the task back in
// the queue, cancels its context and returns, without waiting for the handler. Stop waits for
// the handler, which may still be sending the result.
func TestStop_WaitsForTheHandlerOfATaskAsynqGaveUpOn(t *testing.T) {
	redis := miniredis.RunT(t)
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer target.Close()
	results := &blockingIngester{entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(results.release) }) }
	t.Cleanup(release)
	redisOpt := asynq.RedisClientOpt{Addr: redis.Addr()}
	server := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency:     1,
		Queues:          map[string]int{"healthcheck": 1},
		ShutdownTimeout: 200 * time.Millisecond,
		LogLevel:        asynq.ErrorLevel,
	})
	w := NewWorker(server, newTestHandler(results), zap.NewNop().Sugar())
	require.NoError(t, w.Start())
	client := asynq.NewClient(redisOpt)
	t.Cleanup(func() { _ = client.Close() })
	_, err := client.Enqueue(httpCheckTask(t, target.URL, 5), asynq.Queue("healthcheck"))
	require.NoError(t, err)
	select {
	case <-results.entered:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the worker didn't run the check")
	}

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		w.Stop()
	}()

	// Long enough for asynq to give up on the task and finish its own shutdown
	select {
	case <-stopped:
		require.FailNow(t, "Stop returned while the handler was still running")
	case <-time.After(3 * time.Second):
	}
	release()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "Stop didn't return once the handler did")
	}
}

// A handler with no executor registry panics on the first task it takes, as a bug in a check
// would. The worker's asynq server recovers it, and reports it when an admin opted in.
func startPanickingWorker(t *testing.T) *asynq.Client {
	t.Helper()
	server, client := reportingtest.NewAsynq(t)
	logger := zap.NewNop().Sugar()
	w := NewWorker(server, NewHealthCheckTaskHandler(nil, nil, nil, logger), logger)
	require.NoError(t, w.Start())
	t.Cleanup(w.Stop)
	return client
}

func enqueueCheck(t *testing.T, client *asynq.Client) {
	t.Helper()
	payload, err := json.Marshal(HealthCheckTaskPayload{MonitorID: "mon-1", MonitorType: "http", Interval: 60, ScheduledAt: time.Now()})
	require.NoError(t, err)
	_, err = client.Enqueue(asynq.NewTask(TaskTypeHealthCheck, payload), asynq.MaxRetry(0))
	require.NoError(t, err)
}

func TestWorker_ReportsAPanicInACheckTask(t *testing.T) {
	recorder := reportingtest.Install(t)
	client := startPanickingWorker(t)

	enqueueCheck(t, client)

	events := recorder.Wait(t, 1)
	assert.Equal(t, "asynq", events[0].Tags["component"])
	assert.Equal(t, "runtime.errorString", events[0].Exception[0].Type)
	frames := events[0].Exception[0].Stacktrace.Frames
	assert.Contains(t, frames[len(frames)-1].Function, "GetExecutor", "the frame the panic is in")
	assert.Equal(t, "(*HealthCheckTaskHandler).ProcessTask", frames[len(frames)-2].Function)
}

func TestWorker_ReportsNothingWhileReportingIsOff(t *testing.T) {
	recorder := reportingtest.Install(t)
	reporting.SetEnabled(false)
	client := startPanickingWorker(t)

	enqueueCheck(t, client)

	time.Sleep(500 * time.Millisecond) // the task has been taken and has panicked by now
	assert.Empty(t, recorder.Events())
}
