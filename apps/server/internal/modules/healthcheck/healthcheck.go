package healthcheck

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
