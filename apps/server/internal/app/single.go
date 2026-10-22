package app

import (
	"errors"

	"peekaping/internal/config"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/cleanup"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/scheduler"
	"peekaping/internal/modules/stale"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterSingle provides what cmd/peekaping runs: the HTTP API and the websocket, the scheduler
// and its check pool, the ingest and the stale-monitor watchdog, in one process with no Redis
// and no asynq. Events stay in the process, the stale monitors are kept in memory, the hourly
// cleanup's run lock grants every hour to the one process, and the scheduler is the watchdog's
// stale.Rechecker. The scheduler and the push URL ingest through
// one ingester.NewInOrderService, so the pushes and the checks of a monitor are stored one at a
// time, in order.
func RegisterSingle(container *dig.Container, cfg *config.Config) error {
	if err := Register(container, cfg); err != nil {
		return err
	}
	return errors.Join(
		utils.Provide(container, provideMemoryEventBus, stale.ProvideMemoryStore, cleanup.ProvideMemoryRunLock, provideInOrderIngester),
		scheduler.RegisterDependencies(container),
	)
}

// provideMemoryEventBus provides the event bus that never leaves the process
func provideMemoryEventBus(logger *zap.SugaredLogger) events.EventBus {
	logger.Info("Creating in-memory event bus")
	return events.NewMemoryBus(events.WithLogger(logger.With("component", "memory_event_bus")))
}

// provideInOrderIngester provides the ingester.Service that stores the scheduler's checks and the
// push URL's pushes, one monitor's results at a time, in order
func provideInOrderIngester(
	heartbeatService heartbeat.Service,
	certificateService certificate.Service,
	eventBus events.EventBus,
	logger *zap.SugaredLogger,
) ingester.Service {
	return ingester.NewInOrderService(ingester.NewService(heartbeatService, certificateService, eventBus, logger))
}
