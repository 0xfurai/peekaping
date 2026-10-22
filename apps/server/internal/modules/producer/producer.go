package producer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/worker"
	"peekaping/internal/reporting"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// NewProducer creates a new producer instance. The runner runs the checks it schedules: in
// cluster mode, NewQueueRunner enqueues them for the worker.
func NewProducer(
	rdb *redis.Client,
	runner check.Runner,
	monitorService MonitorFinder,
	loader *check.Loader,
	heartbeatService stale.HeartbeatFinder,
	staleStore stale.Store,
	eventBus events.EventBus,
	leaderElection *LeaderElection,
	cfg *config.Config,
	logger *zap.SugaredLogger,
) *Producer {
	// Use configured concurrency, fallback to constant if not set
	concurrency := cfg.ProducerConcurrency
	if concurrency <= 0 {
		concurrency = ConcurrentProducers
	}

	p := &Producer{
		rdb:                     rdb,
		runner:                  runner,
		monitorService:          monitorService,
		loader:                  loader,
		logger:                  logger.With("component", "producer"),
		monitorIntervals:        make(map[string]int),
		scheduleRefreshInterval: 30 * time.Second, // Refresh schedule every 30 seconds
		reclaimInterval:         ReclaimEvery,
		leadershipCheckInterval: 1 * time.Second,
		leaderElection:          leaderElection,
		concurrency:             concurrency,
	}

	// The watchdog gets stale monitors checked again by scheduling them to run now
	p.watchdog = stale.NewWatchdog(stale.WatchdogConfig{
		Store:      staleStore,
		Rechecker:  p,
		Monitors:   monitorService,
		Heartbeats: heartbeatService,
		EventBus:   eventBus,
		// Redis's clock is the one all producers share
		Now:          func() time.Time { return time.UnixMilli(p.redisNowMs()).UTC() },
		CheckTimeout: worker.HealthCheckTaskTimeout,
		Logger:       logger.With("component", "watchdog"),
	})

	return p
}

// Start starts the producer with leader election. It runs until ctx is done or Stop is called:
// either stops the scheduling and the watchdog, and releases the leadership. When Start fails,
// what had started runs on until Stop.
func (p *Producer) Start(ctx context.Context) error {
	if err := p.begin(ctx); err != nil {
		return err
	}
	p.logger.Info("Starting producer with leader election")

	p.leaderElection.Start(p.ctx)
	p.electionDone = p.leaderElection.Done()

	if err := p.startJobProcessing(); err != nil {
		return fmt.Errorf("failed to start job processing: %w", err)
	}

	// Start a goroutine to monitor leadership changes for monitor syncing
	p.wg.Add(1)
	reporting.Go("producer", p.runLeadershipMonitor)

	p.logger.Info("Producer started successfully")
	return nil
}

// begin gives the producer its lifetime: ctx, ended by Stop too
func (p *Producer) begin(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx != nil {
		return errors.New("producer already started")
	}
	p.ctx, p.cancel = context.WithCancel(ctx)
	return nil
}

// Stop stops the producer and waits for it: the claim loops, the schedule refresher and the
// watchdog end, and the leadership it held is released. It does nothing when the producer didn't
// start, and may be called again.
func (p *Producer) Stop() {
	if p.cancel == nil {
		return
	}
	p.logger.Info("Stopping producer")
	p.cancel()
	p.wg.Wait()
	// The election releases the leadership as its context is done, beside the goroutines above
	if p.electionDone != nil {
		<-p.electionDone
	}
	p.logger.Info("Producer stopped")
}
