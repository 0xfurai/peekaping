package producer

import (
	"context"
	"encoding/json"
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
	// Only process events if we are the leader
	if !el.producer.leaderElection.IsLeader() {
		el.logger.Debugw("Ignoring monitor created event (not leader)")
		return
	}

	// Unmarshal the payload from JSON
	var mon monitor.Model
	if err := el.unmarshalPayload(event.Payload, &mon); err != nil {
		el.logger.Errorw("Failed to unmarshal monitor created event", "error", err)
		return
	}

	el.logger.Infow("Monitor created event received", "monitor_id", mon.ID, "monitor_name", mon.Name)

	ctx := context.Background()
	if err := el.producer.AddMonitor(ctx, mon.ID); err != nil {
		el.logger.Errorw("Failed to add monitor to scheduler",
			"monitor_id", mon.ID,
			"error", err,
		)
	}
}

// handleMonitorUpdated handles monitor updated events
func (el *EventListener) handleMonitorUpdated(event events.Event) {
	// Only process events if we are the leader
	if !el.producer.leaderElection.IsLeader() {
		el.logger.Debugw("Ignoring monitor updated event (not leader)")
		return
	}

	// Unmarshal the payload from JSON
	var mon monitor.Model
	if err := el.unmarshalPayload(event.Payload, &mon); err != nil {
		el.logger.Errorw("Failed to unmarshal monitor updated event", "error", err)
		return
	}

	el.logger.Infow("Monitor updated event received", "monitor_id", mon.ID, "monitor_name", mon.Name)

	ctx := context.Background()
	if err := el.producer.UpdateMonitor(ctx, mon.ID); err != nil {
		el.logger.Errorw("Failed to update monitor in scheduler",
			"monitor_id", mon.ID,
			"error", err,
		)
	}
}

// handleMonitorDeleted handles monitor deleted events
func (el *EventListener) handleMonitorDeleted(event events.Event) {
	// Only process events if we are the leader
	if !el.producer.leaderElection.IsLeader() {
		el.logger.Debugw("Ignoring monitor deleted event (not leader)")
		return
	}

	// Unmarshal the payload from JSON (it's just a string ID)
	var monitorID string
	if err := el.unmarshalPayload(event.Payload, &monitorID); err != nil {
		el.logger.Errorw("Failed to unmarshal monitor deleted event", "error", err)
		return
	}

	el.logger.Infow("Monitor deleted event received", "monitor_id", monitorID)

	ctx := context.Background()
	if err := el.producer.RemoveMonitor(ctx, monitorID); err != nil {
		el.logger.Errorw("Failed to remove monitor from scheduler",
			"monitor_id", monitorID,
			"error", err,
		)
	}
}

// unmarshalPayload unmarshals the event payload from JSON
func (el *EventListener) unmarshalPayload(payload interface{}, target interface{}) error {
	// Payload can be either json.RawMessage or already unmarshaled data
	switch p := payload.(type) {
	case json.RawMessage:
		return json.Unmarshal(p, target)
	case []byte:
		return json.Unmarshal(p, target)
	default:
		// If it's already a Go object, try to marshal and unmarshal it
		// This handles the case where events are published locally without going through Redis
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, target)
	}
}
