package app

import (
	"context"
	"time"

	"peekaping/internal/modules/check"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/healthcheck/supervisor"
	"peekaping/internal/modules/ingester/ingest"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/scheduler"

	"go.uber.org/zap"
)

const (
	// pushCheckConcurrency is how many push monitors are judged at the same time. A push check reads
	// the monitor's latest push and ingests a Down, so a few at a time are plenty.
	pushCheckConcurrency = 16
	// pushReconcileEvery is how often the push monitors' timers are compared with the database. The
	// events the monitors send move them, so this is only the net under a missed one, and each
	// comparison reads every active monitor of the local probe.
	pushReconcileEvery = 5 * time.Minute
	// pushCheckRetryAfter is how long a failed start of the push checks waits before it tries again
	pushCheckRetryAfter = 10 * time.Second
)

// PushChecks judges the push monitors in the cluster on SQLite and PostgreSQL. A push monitor
// runs no check of its own: its check finds that no push arrived within its interval, and ingests a
// Down. The probes are never dealt a push monitor, since pushes arrive at the api. So the api replica
// that holds the leader lock runs the single-mode scheduler over the push monitors alone, ingesting
// through the same in-order service as the push URL, so that a push and the check of its absence are
// stored in turn.
type PushChecks struct {
	source     *scheduler.PushSource
	executors  scheduler.Executors
	supervisor *supervisor.HealthCheckSupervisor
	ingester   ingest.Service
	logger     *zap.SugaredLogger
}

// providePushChecks provides the push monitors' checks of the cluster api
func providePushChecks(
	loader *check.Loader,
	monitors monitor.Service,
	eventBus events.EventBus,
	executors *executor.ExecutorRegistry,
	checks *supervisor.HealthCheckSupervisor,
	ingestService ingest.Service,
	logger *zap.SugaredLogger,
) *PushChecks {
	return &PushChecks{
		source:     scheduler.NewPushSource(scheduler.NewDatabaseSource(monitors, loader, eventBus)),
		executors:  executors,
		supervisor: checks,
		ingester:   ingestService,
		logger:     logger.With("component", "push_checks"),
	}
}

// Run keeps a scheduler of the push monitors going until ctx is done, which is when this replica
// loses the leader lock or stops. Each term of the lock gets a scheduler of its own, which checks
// each push monitor right away, as the scheduler does when it starts, and then on the multiples of
// its interval.
func (p *PushChecks) Run(ctx context.Context) {
	for ctx.Err() == nil {
		s := scheduler.New(scheduler.Config{
			Source:            p.source,
			Executors:         p.executors,
			Supervisor:        p.supervisor,
			Ingester:          p.ingester,
			Concurrency:       pushCheckConcurrency,
			ReconcileInterval: pushReconcileEvery,
			Logger:            p.logger,
		})
		if err := s.Start(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			p.logger.Errorw("Failed to start the push monitors' checks, trying again", "error", err, "retry_in", pushCheckRetryAfter.String())
			select {
			case <-ctx.Done():
				return
			case <-time.After(pushCheckRetryAfter):
			}
			continue
		}
		<-ctx.Done()
		s.Stop()
		p.source.StopWatching()
		return
	}
}
