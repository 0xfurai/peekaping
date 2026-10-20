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
)

// RegisterDependencies registers single mode's scheduler and its stale-monitor watchdog. The
// binary provides the stale.Store they share with the API (stale.ProvideMemoryStore) and the
// ingester.Service. Pushes are ordered with a monitor's checks only when the push endpoint
// ingests through the same ingester.NewInOrderService as the scheduler, so the binary should
// provide that one to both.
func RegisterDependencies(container *dig.Container) {
	container.Provide(ProvideScheduler)
	container.Provide(ProvideWatchdog)
}

// ProvideScheduler provides the scheduler. Its pool runs CHECK_CONCURRENCY checks at a time.
func ProvideScheduler(
	loader *check.Loader,
	monitorService monitor.Service,
	executorRegistry *executor.ExecutorRegistry,
	supervisor *healthcheck.HealthCheckSupervisor,
	ingesterService ingester.Service,
	cfg *config.Config,
	logger *zap.SugaredLogger,
) *Scheduler {
	return New(Config{
		Loader:      loader,
		Monitors:    monitorService,
		Executors:   executorRegistry,
		Supervisor:  supervisor,
		Ingester:    ingesterService,
		Concurrency: cfg.CheckConcurrency,
		Logger:      logger,
	})
}

// ProvideWatchdog provides the stale-monitor watchdog. It records the stale monitors in the
// store and gets them checked again with the scheduler's RunNow. Its Run looks for them
// until its context is done.
func ProvideWatchdog(
	s *Scheduler,
	store stale.Store,
	monitorService monitor.Service,
	heartbeatService heartbeat.Service,
	eventBus events.EventBus,
	logger *zap.SugaredLogger,
) *stale.Watchdog {
	return stale.NewWatchdog(stale.WatchdogConfig{
		Store:        store,
		Rechecker:    s,
		Monitors:     monitorService,
		Heartbeats:   heartbeatService,
		EventBus:     eventBus,
		CheckTimeout: CheckTimeout,
		Logger:       logger.With("component", "watchdog"),
	})
}
