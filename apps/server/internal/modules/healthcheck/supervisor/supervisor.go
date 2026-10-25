// Package supervisor runs one check of a monitor: it bounds the monitor's executor by its timeout,
// and turns what the executor reports into the TickResult that the scheduler ingests. It has no storage or queue behind it, so the remote probe runs its checks with it too.
package supervisor

import (
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck/executor"

	"go.uber.org/zap"
)

type HealthCheckSupervisor struct {
	execRegistry *executor.ExecutorRegistry
	eventBus     events.EventBus
	logger       *zap.SugaredLogger
}

func NewHealthCheck(
	eventBus events.EventBus,
	execRegistry *executor.ExecutorRegistry,
	logger *zap.SugaredLogger,
) *HealthCheckSupervisor {
	return &HealthCheckSupervisor{
		execRegistry: execRegistry,
		eventBus:     eventBus,
		logger:       logger.With("service", "[healthcheck]"),
	}
}
