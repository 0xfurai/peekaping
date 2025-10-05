package producer

import (
	"fmt"
	"time"
)

// runLeadershipMonitor monitors leadership status and starts/stops scheduling accordingly
func (p *Producer) runLeadershipMonitor() {
	defer p.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var isScheduling bool

	for {
		select {
		case <-p.ctx.Done():
			if isScheduling {
				p.logger.Info("Context cancelled, stopping scheduling")
			}
			return
		case <-ticker.C:
			isLeader := p.leaderElection.IsLeader()

			if isLeader && !isScheduling {
				p.logger.Info("Became leader, starting scheduling")
				if err := p.startScheduling(); err != nil {
					p.logger.Errorw("Failed to start scheduling", "error", err)
				} else {
					isScheduling = true
				}
			} else if !isLeader && isScheduling {
				p.logger.Info("Lost leadership, stopping scheduling")
				p.stopScheduling()
				isScheduling = false
			}
		}
	}
}

// startScheduling initializes and starts the scheduling components
func (p *Producer) startScheduling() error {
	// Initialize schedule with active monitors
	if err := p.initializeSchedule(); err != nil {
		return fmt.Errorf("failed to initialize schedule: %w", err)
	}

	// Start background reclaimer (handles crashed or slow producers)
	p.wg.Add(1)
	go p.runReclaimer()

	// Start schedule refresher
	p.wg.Add(1)
	go p.runScheduleRefresher()

	// Start main producer loop
	p.wg.Add(1)
	go p.runProducer()

	return nil
}

// stopScheduling is called when this node loses leadership
// The goroutines will naturally exit when they check the context
func (p *Producer) stopScheduling() {
	// The goroutines will stop when they check ctx.Done()
	// No need to do anything special here as we use the same context
	p.logger.Info("Scheduling stopped due to leadership loss")
}
