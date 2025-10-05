package producer

import (
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
	p.logger.Info("Starting job processing")

	// Start background reclaimer (handles expired leases from any producer)
	p.wg.Add(1)
	go p.runReclaimer()

	// Start main producer loop (all producers can claim and process jobs)
	p.wg.Add(1)
	go p.runProducer()

	p.logger.Info("Job processing started successfully")
	return nil
}

// startMonitorSyncing initializes and starts monitor syncing (leader only)
func (p *Producer) startMonitorSyncing() error {
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
// The goroutines will naturally exit when they check the context
func (p *Producer) stopMonitorSyncing() {
	// The goroutines will stop when they check ctx.Done()
	// No need to do anything special here as we use the same context
	p.logger.Info("Monitor syncing stopped due to leadership loss")
}
