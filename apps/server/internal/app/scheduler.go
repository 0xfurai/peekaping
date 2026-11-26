package app

import (
	"peekaping/internal/config"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/healthcheck/supervisor"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester/ingest"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/scheduler"
	"peekaping/internal/modules/stale"

	"go.uber.org/zap"
)

// The scheduler's providers are single mode's: the scheduler, its source of monitors and the
// stale-monitor watchdog. They sit here, not in package scheduler, since that package is also what
// the remote probe runs its checks with, and a probe has none of the services they need. The
// transport provides the stale.Store the watchdog shares with the API (stale.ProvideMemoryStore)
// and the ingest.Service. Pushes to a push URL are ordered with the monitor's checks only when
// they are ingested through the same ingest.NewInOrderService as the scheduler's, so the binary
// provides that one to both.

// provideSchedulerSource provides the scheduler's source: the database and the event bus
func provideSchedulerSource(
	loader *check.Loader,
	monitorService monitor.Service,
	eventBus events.EventBus,
) scheduler.Source {
	return scheduler.NewDatabaseSource(monitorService, loader, eventBus)
}

// provideScheduler provides the scheduler. Its pool runs CHECK_CONCURRENCY checks at a time.
func provideScheduler(
	source scheduler.Source,
	executorRegistry *executor.ExecutorRegistry,
	checks *supervisor.HealthCheckSupervisor,
	ingesterService ingest.Service,
	cfg *config.Config,
	logger *zap.SugaredLogger,
) *scheduler.Scheduler {
	return scheduler.New(scheduler.Config{
		Source:      source,
		Executors:   executorRegistry,
		Supervisor:  checks,
		Ingester:    ingesterService,
		Concurrency: cfg.CheckConcurrency,
		Logger:      logger,
	})
}

// provideWatchdog provides the stale-monitor watchdog. It records the stale monitors in the
// store and gets those of the local probe checked again with the scheduler's RunNow. It leaves
// alone a monitor whose probe is offline, as probeOffline says, and names the probes that don't run
// a stale monitor's type in its notification, as probes says. Its Run looks for them until its
// context is done.
func provideWatchdog(
	s *scheduler.Scheduler,
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
		CheckTimeout: scheduler.CheckTimeout,
		ProbeOffline: probeOffline,
		Probes:       probes,
		Logger:       logger.With("component", "watchdog"),
	})
}
