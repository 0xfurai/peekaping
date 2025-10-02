package producer

import (
	"context"
	"peekaping/internal/modules/events"

	"go.uber.org/zap"
)

// EventListener listens to monitor events and updates the scheduler
type EventListener struct {
	scheduler *MonitorScheduler
	logger    *zap.SugaredLogger
}

// NewEventListener creates a new event listener
func NewEventListener(scheduler *MonitorScheduler, logger *zap.SugaredLogger) *EventListener {
	return &EventListener{
		scheduler: scheduler,
		logger:    logger.With("component", "producer_event_listener"),
	}
}

// Subscribe subscribes to monitor events
func (el *EventListener) Subscribe(eventBus *events.EventBus) {
	el.logger.Info("Subscribing to monitor events")

	// Subscribe to monitor created events
	eventBus.Subscribe(events.MonitorCreated, func(event events.Event) {
		el.handleMonitorCreated(event)
	})

	// Subscribe to monitor updated events
	eventBus.Subscribe(events.MonitorUpdated, func(event events.Event) {
		el.handleMonitorUpdated(event)
	})

	// Subscribe to monitor deleted events
	eventBus.Subscribe(events.MonitorDeleted, func(event events.Event) {
		el.handleMonitorDeleted(event)
	})

	el.logger.Info("Successfully subscribed to monitor events")
}

// handleMonitorCreated handles monitor created events
func (el *EventListener) handleMonitorCreated(event events.Event) {
	monitorID, ok := event.Payload.(string)
	if !ok {
		el.logger.Errorw("Invalid payload for monitor created event", "payload", event.Payload)
		return
	}

	el.logger.Infow("Monitor created event received", "monitor_id", monitorID)

	ctx := context.Background()
	if err := el.scheduler.AddMonitor(ctx, monitorID); err != nil {
		el.logger.Errorw("Failed to add monitor to scheduler",
			"monitor_id", monitorID,
			"error", err,
		)
	}
}

// handleMonitorUpdated handles monitor updated events
func (el *EventListener) handleMonitorUpdated(event events.Event) {
	monitorID, ok := event.Payload.(string)
	if !ok {
		el.logger.Errorw("Invalid payload for monitor updated event", "payload", event.Payload)
		return
	}

	el.logger.Infow("Monitor updated event received", "monitor_id", monitorID)

	ctx := context.Background()
	if err := el.scheduler.UpdateMonitor(ctx, monitorID); err != nil {
		el.logger.Errorw("Failed to update monitor in scheduler",
			"monitor_id", monitorID,
			"error", err,
		)
	}
}

// handleMonitorDeleted handles monitor deleted events
func (el *EventListener) handleMonitorDeleted(event events.Event) {
	monitorID, ok := event.Payload.(string)
	if !ok {
		el.logger.Errorw("Invalid payload for monitor deleted event", "payload", event.Payload)
		return
	}

	el.logger.Infow("Monitor deleted event received", "monitor_id", monitorID)

	el.scheduler.RemoveMonitor(monitorID)
}
