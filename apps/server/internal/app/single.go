package app

import (
	"peekaping/internal/config"
	"peekaping/internal/modules/cleanup"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/shared"
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
// ingest through one ingest.NewInOrderService, so the pushes and the checks of a monitor are
// stored one at a time, in order.
func RegisterSingle(container *dig.Container, cfg *config.Config) error {
	if err := Register(container, cfg); err != nil {
		return err
	}
	return utils.Provide(container,
		provideMemoryEventBus, stale.ProvideMemoryStore, cleanup.ProvideMemoryRunLock,
		ingester.ProvideLocationStore, provideInOrderIngester,
		provideSchedulerSource, provideScheduler, provideWatchdog,
		provideSingleMode, provideSingleClaimer, provideSingleWindow, provideSingleLocalProbe,
		probe.ProvideSweep,
	)
}

// provideSingleMode tells the usage ping that this install runs in single mode
func provideSingleMode() telemetry.Mode { return telemetry.ModeSingle }

// provideSingleClaimer provides the claimer of the usage ping: the one process always sends
func provideSingleClaimer() telemetry.Claimer { return telemetry.AlwaysClaimer{} }

// provideSingleLocalProbe provides how the local probe is known to be online: it is the process
// itself, which runs its checks, and is never offline
func provideSingleLocalProbe() shared.LocalProbeLiveness { return shared.LocalProbeIsTheProcess }

// provideSingleWindow provides the duplicate window of the probe protocol's results route in the
// process's memory: there is one process, so nothing else needs to see it
func provideSingleWindow() protocol.Window { return protocol.NewMemoryWindow() }

// provideMemoryEventBus provides the event bus that never leaves the process
func provideMemoryEventBus(logger *zap.SugaredLogger) events.EventBus {
	logger.Info("Creating in-memory event bus")
	return events.NewMemoryBus(events.WithLogger(logger.With("component", "memory_event_bus")))
}
