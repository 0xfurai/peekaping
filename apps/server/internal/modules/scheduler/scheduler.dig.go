package scheduler

import (
	"go.uber.org/dig"
	"go.uber.org/zap"

	"peekaping/internal/config"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/stale"
	"peekaping/internal/utils"
)

// RegisterDependencies registers single mode's scheduler, its source of monitors and its
// stale-monitor watchdog. The binary provides the stale.Store they share with the API
// (stale.ProvideMemoryStore) and the ingester.Service. Pushes to a push URL are ordered with the monitor's checks only when they
// are ingested through the same ingester.NewInOrderService as the scheduler's, so the binary
// should provide that one to both.
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, ProvideSource, ProvideScheduler, ProvideWatchdog)
}

// ProvideSource provides the scheduler's source: the database and the event bus
func ProvideSource(
	loader *check.Loader,
	monitorService monitor.Service,
	eventBus events.EventBus,
) Source {
	return NewDatabaseSource(monitorService, loader, eventBus)
}

// ProvideScheduler provides the scheduler. Its pool runs CHECK_CONCURRENCY checks at a time.
func ProvideScheduler(
	source Source,
	executorRegistry *executor.ExecutorRegistry,
	supervisor *healthcheck.HealthCheckSupervisor,
	ingesterService ingester.Service,
	cfg *config.Config,
	logger *zap.SugaredLogger,
) *Scheduler {
	return New(Config{
		Source:      source,
		Executors:   executorRegistry,
		Supervisor:  supervisor,
		Ingester:    ingesterService,
		Concurrency: cfg.CheckConcurrency,
		Logger:      logger,
	})
}

// ProvideWatchdog provides the stale-monitor watchdog. It records the stale monitors in the
// store and gets those of the local probe checked again with the scheduler's RunNow. It leaves
// alone a monitor whose probe is offline, as probeOffline says, and names the probes that don't run
// a stale monitor's type in its notification, as probes says. Its Run looks for them until its
// context is done.
func ProvideWatchdog(
	s *Scheduler,
	store stale.Store,
	monitorService monitor.Service,
	heartbeatService heartbeat.Service,
	eventBus events.EventBus,
	probeOffline stale.ProbeOfflineFunc,
	probes stale.ProbeLookup,
	logger *zap.SugaredLogger,
) *stale.Watchdog {
	return stale.NewWatchdog(stale.WatchdogConfig{
		Store:        store,
		Rechecker:    s,
		Monitors:     monitorService,
		Heartbeats:   heartbeatService,
		EventBus:     eventBus,
		CheckTimeout: CheckTimeout,
		ProbeOffline: probeOffline,
		Probes:       probes,
		Logger:       logger.With("component", "watchdog"),
	})
}
