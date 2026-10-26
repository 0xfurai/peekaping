package probeclient_test

import (
	"context"
	"testing"

	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/probeclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeExecutor is an executor that checks nothing
type fakeExecutor struct{ name string }

func (fakeExecutor) Execute(context.Context, *executor.Monitor, *executor.Proxy) *executor.Result {
	return &executor.Result{Status: shared.MonitorStatusUp, Message: "ran"}
}
func (fakeExecutor) Validate(string) error         { return nil }
func (fakeExecutor) Unmarshal(string) (any, error) { return nil, nil }

// fakeRegistry holds the executors of the types a probe was built with
type fakeRegistry map[string]executor.Executor

func (r fakeRegistry) GetExecutor(monitorType string) (executor.Executor, bool) {
	e, ok := r[monitorType]
	return e, ok
}

func (r fakeRegistry) Types() []string {
	types := make([]string, 0, len(r))
	for monitorType := range r {
		types = append(types, monitorType)
	}
	return types
}

func newRegistry(types ...string) fakeRegistry {
	registry := fakeRegistry{}
	for _, monitorType := range types {
		registry[monitorType] = fakeExecutor{name: monitorType}
	}
	return registry
}

func TestExecutors_UnknownTypeIsDown(t *testing.T) {
	executors := probeclient.NewExecutors(newRegistry("http", "tcp"))

	t.Run("a type the probe has an executor for is run by it", func(t *testing.T) {
		got, ok := executors.GetExecutor("tcp")

		require.True(t, ok)
		assert.Equal(t, fakeExecutor{name: "tcp"}, got)
	})

	t.Run("a type it has none for is Down, and says so", func(t *testing.T) {
		got, ok := executors.GetExecutor("kafka-producer")
		require.True(t, ok, "the scheduler still gets an executor, so the gap shows as a result")

		result := got.Execute(context.Background(), &executor.Monitor{ID: "m1", Type: "kafka-producer"}, nil)

		require.NotNil(t, result)
		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Equal(t, "monitor type kafka-producer not supported by this probe", result.Message)
		assert.False(t, result.StartTime.IsZero())
		assert.False(t, result.EndTime.Before(result.StartTime))
	})

	t.Run("a push monitor is never run, even when the registry has the executor", func(t *testing.T) {
		got, ok := probeclient.NewExecutors(newRegistry("http", "push")).GetExecutor("push")
		require.True(t, ok)

		result := got.Execute(context.Background(), &executor.Monitor{ID: "m2", Type: "push"}, nil)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Equal(t, "monitor type push not supported by this probe", result.Message)
	})
}

func TestSupportedTypes_LeaveOutPush(t *testing.T) {
	executors := probeclient.NewExecutors(newRegistry("tcp", "push", "http", "ping"))

	assert.Equal(t, []string{"http", "ping", "tcp"}, executors.SupportedTypes())
}
