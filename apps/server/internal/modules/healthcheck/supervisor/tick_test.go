package supervisor

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// stubExecutor returns result after delay, or once ctx is done if honorCtx is set.
type stubExecutor struct {
	delay    time.Duration
	honorCtx bool
	panics   bool
	result   *executor.Result
}

func (e *stubExecutor) Execute(ctx context.Context, _ *shared.Monitor, _ *executor.Proxy) *executor.Result {
	if e.panics {
		panic("executor bug")
	}
	if e.honorCtx {
		select {
		case <-time.After(e.delay):
		case <-ctx.Done():
		}
	} else {
		time.Sleep(e.delay)
	}
	return e.result
}

func (e *stubExecutor) Validate(string) error         { return nil }
func (e *stubExecutor) Unmarshal(string) (any, error) { return nil, nil }

func TestHandleMonitorTickDeadline(t *testing.T) {
	supervisor := NewHealthCheck(nil, nil, zap.NewNop().Sugar())
	m := &shared.Monitor{ID: "mon-1", Name: "Test", Timeout: 1}
	executorResult := &executor.Result{Status: shared.MonitorStatusDown, Message: "executor message"}

	t.Run("returns the executor result when it finishes in time", func(t *testing.T) {
		up := &executor.Result{Status: shared.MonitorStatusUp, Message: "ok"}
		tick := supervisor.HandleMonitorTick(context.Background(), m, &stubExecutor{result: up}, nil, false)
		require.NotNil(t, tick)
		assert.Equal(t, up, tick.ExecutionResult)
	})

	t.Run("keeps the executor's own message when it honors the timeout", func(t *testing.T) {
		exec := &stubExecutor{delay: time.Minute, honorCtx: true, result: executorResult}
		tick := supervisor.HandleMonitorTick(context.Background(), m, exec, nil, false)
		require.NotNil(t, tick)
		assert.Equal(t, "executor message", tick.ExecutionResult.Message)
	})

	t.Run("reports DOWN when the executor ignores the timeout", func(t *testing.T) {
		exec := &stubExecutor{delay: time.Minute, result: &executor.Result{Status: shared.MonitorStatusUp}}

		start := time.Now()
		tick := supervisor.HandleMonitorTick(context.Background(), m, exec, nil, false)
		elapsed := time.Since(start)

		require.NotNil(t, tick)
		assert.Equal(t, shared.MonitorStatusDown, tick.ExecutionResult.Status)
		assert.Contains(t, tick.ExecutionResult.Message, "did not finish within the 1s timeout")
		assert.Less(t, elapsed, time.Second+ExecutorGrace+time.Second)
	})

	t.Run("reports DOWN instead of crashing when the executor panics", func(t *testing.T) {
		tick := supervisor.HandleMonitorTick(context.Background(), m, &stubExecutor{panics: true}, nil, false)

		require.NotNil(t, tick)
		assert.Equal(t, shared.MonitorStatusDown, tick.ExecutionResult.Status)
		assert.Contains(t, tick.ExecutionResult.Message, "executor bug")
	})
}
