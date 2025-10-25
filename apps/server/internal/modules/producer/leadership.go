package producer

import (
	"context"
	"fmt"
	"time"
)

// runLeadershipMonitor monitors leadership status and starts/stops monitor syncing accordingly
// Note: Job processing (runProducer, runReclaimer) runs on all producers regardless of leadership
func (p *Producer) runLeadershipMonitor() {
	defer p.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var isSyncing bool

	for {
		select {
		case <-p.ctx.Done():
			if isSyncing {
				p.logger.Info("Context cancelled, stopping monitor syncing")
			}
			return
		case <-ticker.C:
			isLeader := p.leaderElection.IsLeader()

			if isLeader && !isSyncing {
				p.logger.Info("Became leader, starting monitor syncing")
				if err := p.startMonitorSyncing(); err != nil {
					p.logger.Errorw("Failed to start monitor syncing", "error", err)
				} else {
					isSyncing = true
				}
			} else if !isLeader && isSyncing {
				p.logger.Info("Lost leadership, stopping monitor syncing")
				p.stopMonitorSyncing()
				isSyncing = false
			}
		}
	}
}

// startJobProcessing starts job processing tasks (runs on all producers)
func (p *Producer) startJobProcessing() error {
	p.logger.Infow("Starting job processing", "concurrent_producers", p.concurrency)

	// Start background reclaimer (handles expired leases from any producer)
	p.wg.Add(1)
	go p.runReclaimer()

	// Start multiple producer goroutines for concurrent processing
	// Each goroutine independently claims and processes batches of monitors
	for i := 0; i < p.concurrency; i++ {
		p.wg.Add(1)
		go func(workerID int) {
			p.logger.Infow("Starting producer worker", "worker_id", workerID)
			if err := p.runProducer(workerID); err != nil {
				p.logger.Errorw("Producer worker exited with error", "worker_id", workerID, "error", err)
			} else {
				p.logger.Infow("Producer worker stopped gracefully", "worker_id", workerID)
			}
		}(i)
	}

	p.logger.Info("Job processing started successfully")
	return nil
}

// startMonitorSyncing initializes and starts monitor syncing (leader only)
func (p *Producer) startMonitorSyncing() error {
	// Create a new context for monitor syncing that can be cancelled independently
	p.syncCtx, p.syncCancel = context.WithCancel(p.ctx)

	// Initialize schedule with active monitors from database
	if err := p.initializeSchedule(); err != nil {
		return fmt.Errorf("failed to initialize schedule: %w", err)
	}

	// Start schedule refresher to keep Redis in sync with database
	p.wg.Add(1)
	go p.runScheduleRefresher()

	p.logger.Info("Monitor syncing started successfully")
	return nil
}

// stopMonitorSyncing is called when this node loses leadership
func (p *Producer) stopMonitorSyncing() {
	// Cancel the sync context to stop all monitor syncing goroutines
	if p.syncCancel != nil {
		p.logger.Info("Stopping monitor syncing due to leadership loss")
		p.syncCancel()
		p.syncCancel = nil
		p.syncCtx = nil
	}
}
