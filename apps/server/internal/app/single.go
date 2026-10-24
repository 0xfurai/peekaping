package app

import (
	"errors"

	"peekaping/internal/config"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/cleanup"
	"peekaping/internal/modules/error_reporting"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/scheduler"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/telemetry"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterSingle provides what cmd/peekaping runs: the HTTP API and the websocket, the scheduler
// and its check pool, the ingest, the stale-monitor watchdog and the sweep that sets silent
// probes offline, in one process with no Redis and no asynq. Events stay in the process, the
// stale monitors are kept in memory, the hourly cleanup's run lock grants every hour to the one
// process, and the scheduler is the watchdog's stale.Rechecker. The scheduler and the push URL
// ingest through one ingester.NewInOrderService, so the pushes and the checks of a monitor are
// stored one at a time, in order.
func RegisterSingle(container *dig.Container, cfg *config.Config) error {
	if err := Register(container, cfg); err != nil {
		return err
	}
	return errors.Join(
		utils.Provide(container, provideMemoryEventBus, stale.ProvideMemoryStore, cleanup.ProvideMemoryRunLock, ingester.ProvideLocationStore, provideInOrderIngester, provideSingleMode, provideSingleClaimer, provideSingleSwitch, probe.ProvideSweep),
		scheduler.RegisterDependencies(container),
	)
}

// provideSingleMode tells the usage ping that this install runs in single mode
func provideSingleMode() telemetry.Mode { return telemetry.ModeSingle }

// provideSingleClaimer provides the claimer of the usage ping: the one process always sends
func provideSingleClaimer() telemetry.Claimer { return telemetry.AlwaysClaimer{} }

// provideSingleSwitch provides the error reporting switch: the one process is the only one to tell
func provideSingleSwitch() error_reporting.Switch { return error_reporting.LocalSwitch{} }

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
	locations ingester.LocationStore,
	eventBus events.EventBus,
	logger *zap.SugaredLogger,
) ingester.Service {
	return ingester.NewInOrderService(ingester.NewService(heartbeatService, certificateService, locations, eventBus, logger))
}
