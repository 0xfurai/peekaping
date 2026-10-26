package probeclient

import (
	"context"
	"fmt"
	"slices"
	"time"

	"peekaping/internal/modules/healthcheck/executor"
)

// pushType is the monitor type whose "check" is reading the time of the last push from the
// database, which stays in the API. A probe never runs one.
const pushType = "push"

// Registry is the executors a probe was built with. executor.ExecutorRegistry is one.
type Registry interface {
	// GetExecutor returns the executor of a monitor type
	GetExecutor(monitorType string) (executor.Executor, bool)
	// Types lists the monitor types the registry has an executor for
	Types() []string
}

// Executors is what the probe's scheduler finds its executors in: the registry's, except that
// every type it can't run has one that reports the gap. A monitor of a type the probe has no
// executor for, or a push monitor, gets a Down result saying so, instead of no check at all.
type Executors struct {
	registry Registry
}

// NewExecutors wraps the registry
func NewExecutors(registry Registry) *Executors {
	return &Executors{registry: registry}
}

// GetExecutor returns the executor of the monitor type. It never reports a type missing.
func (e *Executors) GetExecutor(monitorType string) (executor.Executor, bool) {
	if monitorType != pushType {
		if found, ok := e.registry.GetExecutor(monitorType); ok {
			return found, true
		}
	}
	return unsupported{monitorType: monitorType}, true
}

// SupportedTypes are the monitor types the probe runs, sorted: every type of the registry but push.
// It is what hello tells the server.
func (e *Executors) SupportedTypes() []string {
	types := slices.DeleteFunc(slices.Clone(e.registry.Types()), func(monitorType string) bool { return monitorType == pushType })
	slices.Sort(types)
	return types
}

// unsupported is the executor of a monitor type the probe doesn't run: its check is Down
type unsupported struct {
	monitorType string
}

func (u unsupported) Execute(_ context.Context, _ *executor.Monitor, _ *executor.Proxy) *executor.Result {
	now := time.Now().UTC()
	return executor.DownResult(u.err(), now, now)
}

func (u unsupported) Validate(string) error { return u.err() }

func (u unsupported) Unmarshal(string) (any, error) { return nil, u.err() }

func (u unsupported) err() error {
	return fmt.Errorf("monitor type %s not supported by this probe", u.monitorType)
}
