package supervisor

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/reporting"
	"peekaping/internal/reporting/reportingtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestSupervisor() (*HealthCheckSupervisor, *shared.Monitor) {
	return NewHealthCheck(nil, nil, zap.NewNop().Sugar()), &shared.Monitor{ID: "mon-1", Name: "Test", Timeout: 1}
}

func TestHandleMonitorTick_ReportsAnExecutorPanicAndStillReportsDown(t *testing.T) {
	recorder := reportingtest.Install(t)
	supervisor, m := newTestSupervisor()

	tick := supervisor.HandleMonitorTick(context.Background(), m, &stubExecutor{panics: true}, nil, false)

	require.NotNil(t, tick)
	assert.Equal(t, shared.MonitorStatusDown, tick.ExecutionResult.Status, "the check is still recovered")
	events := recorder.Events()
	require.Len(t, events, 1)
	assert.Equal(t, "executor", events[0].Tags["component"])
	assert.Equal(t, "string", events[0].Exception[0].Type)
	frames := events[0].Exception[0].Stacktrace.Frames
	assert.Equal(t, "(*stubExecutor).Execute", frames[len(frames)-1].Function)
	assert.Empty(t, events[0].Exception[0].Value, "the panic's message is not sent")
}

func TestHandleMonitorTick_ReportsNothingForAnExecutorPanicWhileReportingIsOff(t *testing.T) {
	recorder := reportingtest.Install(t)
	reporting.SetEnabled(false)
	supervisor, m := newTestSupervisor()

	tick := supervisor.HandleMonitorTick(context.Background(), m, &stubExecutor{panics: true}, nil, false)

	require.NotNil(t, tick)
	assert.Equal(t, shared.MonitorStatusDown, tick.ExecutionResult.Status)
	assert.Empty(t, recorder.Events())
}

// A target that is down is the product working: only a panic is reported
func TestHandleMonitorTick_ReportsNothingForAFailedCheck(t *testing.T) {
	recorder := reportingtest.Install(t)
	supervisor, m := newTestSupervisor()
	now := time.Now().UTC()

	for name, exec := range map[string]executor.Executor{
		"a Down result":          &stubExecutor{result: executor.DownResult(assert.AnError, now, now)},
		"a timeout":              &stubExecutor{delay: time.Minute, honorCtx: true, result: executor.DownResult(context.DeadlineExceeded, now, now)},
		"an executor that hangs": &stubExecutor{delay: time.Minute, result: &executor.Result{Status: shared.MonitorStatusUp}},
	} {
		t.Run(name, func(t *testing.T) {
			tick := supervisor.HandleMonitorTick(context.Background(), m, exec, nil, false)

			require.NotNil(t, tick)
			assert.Equal(t, shared.MonitorStatusDown, tick.ExecutionResult.Status)
			assert.Empty(t, recorder.Events())
		})
	}
}
