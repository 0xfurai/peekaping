package ingester

import (
	"vigi/internal/modules/certificate"
	"vigi/internal/modules/events"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/monitor_maintenance"

	"github.com/hibiken/asynq"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterDependencies registers ingester dependencies in the DI container
func RegisterDependencies(container *dig.Container) {
	// Provide ingester task handler
	container.Provide(ProvideIngesterTaskHandler)

	// Provide ingester
	container.Provide(ProvideIngester)
}

// ProvideIngesterTaskHandler provides an ingester task handler
func ProvideIngesterTaskHandler(
	heartbeatService heartbeat.Service,
	certificateService certificate.Service,
	monitorMaintenanceService monitor_maintenance.Service,
	eventBus events.EventBus,
	logger *zap.SugaredLogger,
) *IngesterTaskHandler {
	return NewIngesterTaskHandler(
		heartbeatService,
		certificateService,
		monitorMaintenanceService,
		eventBus,
		logger,
	)
}

// ProvideIngester provides an ingester
func ProvideIngester(
	server *asynq.Server,
	handler *IngesterTaskHandler,
	logger *zap.SugaredLogger,
) *Ingester {
	return NewIngester(server, handler, logger)
}
