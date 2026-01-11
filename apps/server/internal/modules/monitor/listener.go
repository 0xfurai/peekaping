package monitor

import (
	"context"
	"vigi/internal/infra"
	"vigi/internal/modules/events"
	"vigi/internal/modules/heartbeat"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

// MonitorEventListener handles monitor status change events
type MonitorEventListener struct {
	monitorService Service
	logger         *zap.SugaredLogger
}

type MonitorEventListenerParams struct {
	dig.In
	MonitorService Service
	Logger         *zap.SugaredLogger
}

func NewMonitorEventListener(p MonitorEventListenerParams) *MonitorEventListener {
	return &MonitorEventListener{
		monitorService: p.MonitorService,
		logger:         p.Logger.Named("[monitor-event-listener]"),
	}
}

// Subscribe subscribes to MonitorStatusChanged events
func (l *MonitorEventListener) Subscribe(eventBus events.EventBus) {
	eventBus.Subscribe(events.MonitorStatusChanged, l.handleMonitorStatusChanged)
}

func (l *MonitorEventListener) handleMonitorStatusChanged(event events.Event) {
	ctx := context.Background()

	hb, ok := infra.UnmarshalEventPayload[heartbeat.Model](event)
	if !ok {
		l.logger.Errorf("Failed to unmarshal heartbeat event payload")
		return
	}

	monitorID := hb.MonitorID
	newStatus := hb.Status

	l.logger.Infof("Monitor status changed event received for monitor: %s, new status: %d", monitorID, newStatus)

	// Get the current monitor to check if status actually changed
	// Passing empty string for orgID as this is an internal listener updating system state
	currentMonitor, err := l.monitorService.FindByID(ctx, monitorID, "")
	if err != nil {
		l.logger.Errorf("Failed to get monitor %s: %v", monitorID, err)
		return
	}

	if currentMonitor == nil {
		l.logger.Warnf("Monitor %s not found", monitorID)
		return
	}

	// Only update if status actually changed
	if currentMonitor.Status == newStatus {
		l.logger.Debugf("Monitor %s status unchanged (%d), skipping update", monitorID, newStatus)
		return
	}

	// Update monitor status in database
	updateModel := &PartialUpdateDto{
		Status: &newStatus,
	}

	_, err = l.monitorService.UpdatePartial(ctx, monitorID, updateModel, true, "")
	if err != nil {
		l.logger.Errorf("Failed to update monitor %s status to %d: %v", monitorID, newStatus, err)
		return
	}

	l.logger.Infof("Successfully updated monitor %s status from %d to %d", monitorID, currentMonitor.Status, newStatus)
}
