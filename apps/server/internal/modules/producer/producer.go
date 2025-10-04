package producer

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// Producer manages the monitor scheduling with leader election
type Producer struct {
	election      *LeaderElection
	scheduler     *MonitorScheduler
	eventListener *EventListener
	logger        *zap.SugaredLogger
	syncInterval  time.Duration
	stopChan      chan struct{}
	doneChan      chan struct{}
}

// NewProducer creates a new producer
func NewProducer(
	election *LeaderElection,
	scheduler *MonitorScheduler,
	eventListener *EventListener,
	logger *zap.SugaredLogger,
) *Producer {
	return &Producer{
		election:      election,
		scheduler:     scheduler,
		eventListener: eventListener,
		logger:        logger.With("component", "producer"),
		syncInterval:  5 * time.Minute, // Sync every 5 minutes
		stopChan:      make(chan struct{}),
		doneChan:      make(chan struct{}),
	}
}

// Start starts the producer
func (p *Producer) Start(ctx context.Context) error {
	p.logger.Info("Starting producer")

	// Start leader election
	p.election.Start(ctx)

	// Start the monitoring goroutine
	go p.monitorLeadership(ctx)

	p.logger.Info("Producer started successfully")
	return nil
}

// Stop stops the producer
func (p *Producer) Stop() {
	p.logger.Info("Stopping producer")
	close(p.stopChan)
	<-p.doneChan

	// Stop the scheduler
	p.scheduler.Stop()

	// Stop leader election
	p.election.Stop()

	p.logger.Info("Producer stopped")
}

// monitorLeadership monitors leadership status and manages scheduler accordingly
func (p *Producer) monitorLeadership(ctx context.Context) {
	defer close(p.doneChan)

	var wasLeader bool
	var schedulerStarted bool
	ticker := time.NewTicker(p.syncInterval)
	defer ticker.Stop()

	for {
		isLeader := p.election.IsLeader()

		// Check if leadership status changed
		if isLeader != wasLeader {
			if isLeader {
				p.logger.Info("Became leader, starting scheduler and syncing monitors")

				// Start the scheduler (this runs in a goroutine internally)
				if !schedulerStarted {
					go func() {
						if err := p.scheduler.Start(); err != nil {
							p.logger.Errorw("Failed to start scheduler", "error", err)
						}
					}()
					schedulerStarted = true
				}

				// Sync monitors to register all active monitors
				if err := p.scheduler.SyncMonitors(ctx); err != nil {
					p.logger.Errorw("Failed to sync monitors", "error", err)
				}
			} else {
				p.logger.Info("Lost leadership")
				// Note: Asynq scheduler handles distributed coordination internally
				// Multiple schedulers can run, but only one will enqueue tasks
			}
			wasLeader = isLeader
		}

		// If we are the leader, periodically sync monitors
		if isLeader {
			select {
			case <-ticker.C:
				p.logger.Debug("Periodic monitor sync")
				if err := p.scheduler.SyncMonitors(ctx); err != nil {
					p.logger.Errorw("Failed to sync monitors", "error", err)
				}

				// Log stats
				stats := p.scheduler.GetStats()
				p.logger.Infow("Scheduler stats", "stats", stats)

			case <-p.stopChan:
				return
			case <-ctx.Done():
				return
			}
		} else {
			// Not leader, just wait
			select {
			case <-time.After(1 * time.Second):
				continue
			case <-p.stopChan:
				return
			case <-ctx.Done():
				return
			}
		}
	}
}

// IsLeader returns whether this producer instance is the leader
func (p *Producer) IsLeader() bool {
	return p.election.IsLeader()
}

// SyncMonitors manually triggers a monitor sync (useful for testing or manual operations)
func (p *Producer) SyncMonitors(ctx context.Context) error {
	if !p.election.IsLeader() {
		return fmt.Errorf("not the leader, cannot sync monitors")
	}
	return p.scheduler.SyncMonitors(ctx)
}
