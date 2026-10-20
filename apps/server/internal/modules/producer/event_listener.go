package producer

import (
	"context"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"

	"go.uber.org/zap"
)

// EventListener listens to monitor events and updates the scheduler
type EventListener struct {
	producer *Producer
	logger   *zap.SugaredLogger
}

// NewEventListener creates a new event listener
func NewEventListener(producer *Producer, logger *zap.SugaredLogger) *EventListener {
	return &EventListener{
		producer: producer,
		logger:   logger.With("component", "producer_event_listener"),
	}
}

// Subscribe subscribes to monitor events
func (el *EventListener) Subscribe(eventBus events.EventBus) {
	el.logger.Info("Subscribing to monitor events")

	subscriber := events.NewSubscriber(eventBus, "producer")
	events.Subscribe(subscriber, events.MonitorCreated, el.handleMonitorCreated)
	events.Subscribe(subscriber, events.MonitorUpdated, el.handleMonitorUpdated)
	events.Subscribe(subscriber, events.MonitorDeleted, el.handleMonitorDeleted)

	el.logger.Info("Successfully subscribed to monitor events")
}

// handleMonitorCreated handles monitor created events
func (el *EventListener) handleMonitorCreated(ctx context.Context, mon monitor.Model) {
	// Only process events if we are the leader
	if !el.producer.leaderElection.IsLeader() {
		el.logger.Debugw("Ignoring monitor created event (not leader)")
		return
	}

	el.logger.Infow("Monitor created event received", "monitor_id", mon.ID, "monitor_name", mon.Name)

	if err := el.producer.AddMonitor(ctx, mon.ID); err != nil {
		el.logger.Errorw("Failed to add monitor to scheduler",
			"monitor_id", mon.ID,
			"error", err,
		)
	}
}

// handleMonitorUpdated handles monitor updated events
func (el *EventListener) handleMonitorUpdated(ctx context.Context, mon monitor.Model) {
	// Only process events if we are the leader
	if !el.producer.leaderElection.IsLeader() {
		el.logger.Debugw("Ignoring monitor updated event (not leader)")
		return
	}

	el.logger.Infow("Monitor updated event received", "monitor_id", mon.ID, "monitor_name", mon.Name)

	if err := el.producer.UpdateMonitor(ctx, mon.ID); err != nil {
		el.logger.Errorw("Failed to update monitor in scheduler",
			"monitor_id", mon.ID,
			"error", err,
		)
	}
}

// handleMonitorDeleted handles monitor deleted events
func (el *EventListener) handleMonitorDeleted(ctx context.Context, monitorID string) {
	// Only process events if we are the leader
	if !el.producer.leaderElection.IsLeader() {
		el.logger.Debugw("Ignoring monitor deleted event (not leader)")
		return
	}

	el.logger.Infow("Monitor deleted event received", "monitor_id", monitorID)

	if err := el.producer.RemoveMonitor(ctx, monitorID); err != nil {
		el.logger.Errorw("Failed to remove monitor from scheduler",
			"monitor_id", monitorID,
			"error", err,
		)
	}
}
