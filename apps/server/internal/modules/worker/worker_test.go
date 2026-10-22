package worker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"peekaping/internal/reporting"
	"peekaping/internal/reporting/reportingtest"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// A handler with no executor registry panics on the first task it takes, as a bug in a check
// would. The worker's asynq server recovers it, and reports it when an admin opted in.
func startPanickingWorker(t *testing.T) *asynq.Client {
	t.Helper()
	server, client := reportingtest.NewAsynq(t)
	logger := zap.NewNop().Sugar()
	w := NewWorker(server, NewHealthCheckTaskHandler(nil, nil, nil, logger), logger)
	require.NoError(t, w.Start(context.Background()))
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
