package producer

import (
	"context"
	"errors"
	"fmt"

	"peekaping/internal/reporting"
)

// runLeadershipMonitor runs monitor syncing while this node is the leader: it starts when the node
// gets the leadership and stops when it loses it, and stops when the producer does.
// Note: Job processing (runProducer, runReclaimer) runs on all producers regardless of leadership
func (p *Producer) runLeadershipMonitor() {
	defer p.wg.Done()

	p.leaderElection.RunWhileLeader(p.ctx, "monitor syncing", p.leadershipCheckInterval, p.syncWhileLeader)
}

// syncWhileLeader starts monitor syncing, and returns once ctx is done: the node lost the
// leadership, or the producer is stopping. What syncing started ends with ctx, on goroutines that
// Stop waits for.
func (p *Producer) syncWhileLeader(ctx context.Context) error {
	if err := p.startMonitorSyncing(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

// startJobProcessing starts job processing tasks (runs on all producers)
func (p *Producer) startJobProcessing() error {
	p.logger.Infow("Starting job processing", "concurrent_producers", p.concurrency)

	// Start background reclaimer (handles expired leases from any producer)
	p.wg.Add(1)
	reporting.Go("producer", p.runReclaimer)

	// Start multiple producer goroutines for concurrent processing
	// Each goroutine independently claims and processes batches of monitors
	for i := 0; i < p.concurrency; i++ {
		p.wg.Add(1)
		workerID := i
		reporting.Go("producer", func() {
			p.logger.Infow("Starting producer worker", "worker_id", workerID)
			// A worker ends with its context's error when the producer stops: that is no failure
			if err := p.runProducer(workerID); err != nil && !errors.Is(err, context.Canceled) {
				p.logger.Errorw("Producer worker exited with error", "worker_id", workerID, "error", err)
			} else {
				p.logger.Infow("Producer worker stopped gracefully", "worker_id", workerID)
			}
		})
	}

	p.logger.Info("Job processing started successfully")
	return nil
}

// startMonitorSyncing initializes and starts monitor syncing (leader only), which ends with ctx
func (p *Producer) startMonitorSyncing(syncCtx context.Context) error {
	// Initialize schedule with active monitors from database
	if err := p.initializeSchedule(); err != nil {
		return fmt.Errorf("failed to initialize schedule: %w", err)
	}

	// Start schedule refresher to keep Redis in sync with database
	p.wg.Add(1)
	reporting.Go("producer", func() { p.runScheduleRefresher(syncCtx) })

	// Start the stale-monitor watchdog
	p.watchdogWg.Add(1)
	reporting.Go("watchdog", func() {
		defer p.watchdogWg.Done()
		p.watchdog.Run(syncCtx)
	})

	// Start the sweep that sets silent probes offline. Like the watchdog it runs on the leader
	// only, so that each probe is announced offline once.
	if p.probeSweep != nil {
		p.wg.Add(1)
		reporting.Go("probe-sweep", func() {
			defer p.wg.Done()
			p.probeSweep.Run(syncCtx)
		})
	}

	p.logger.Info("Monitor syncing started successfully")
	return nil
}
