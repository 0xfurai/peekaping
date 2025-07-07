package healthcheck

import (
	"context"
	"fmt"
	"peekaping/src/modules/events"
	"peekaping/src/modules/shared"
)

// EventListener handles monitor events and manages health check polling
type EventListener struct {
	supervisor *HealthCheckSupervisor
}

// NewEventListener creates a new event listener
func NewEventListener(supervisor *HealthCheckSupervisor) *EventListener {
	return &EventListener{
		supervisor: supervisor,
	}
}

// Start subscribes to monitor events
func (l *EventListener) Start(eventBus *events.EventBus) {
	// Subscribe to monitor events
	eventBus.Subscribe(events.MonitorCreated, l.handleMonitorCreated)
	eventBus.Subscribe(events.MonitorUpdated, l.handleMonitorUpdated)
	eventBus.Subscribe(events.MonitorDeleted, l.handleMonitorDeleted)
	eventBus.Subscribe(events.ProxyUpdated, l.handleProxyUpdated)
	eventBus.Subscribe(events.ProxyDeleted, l.handleProxyDeleted)
}

// handleMonitorCreated starts health check polling for newly created monitors
func (l *EventListener) handleMonitorCreated(event events.Event) {
	monitor, ok := event.Payload.(*shared.Monitor)
	if !ok {
		fmt.Printf("Invalid payload type for monitor.created event: %T\n", event.Payload)
		return
	}

	if monitor.Active {
		ctx := context.Background()
		if err := l.supervisor.StartMonitor(ctx, monitor, false); err != nil {
			fmt.Printf("Failed to start health check for monitor %s: %v\n", monitor.ID, err)
		}
	}
}

// handleMonitorUpdated manages health check polling based on monitor updates
func (l *EventListener) handleMonitorUpdated(event events.Event) {
	monitor, ok := event.Payload.(*shared.Monitor)
	if !ok {
		fmt.Printf("Invalid payload type for monitor.updated event: %T\n", event.Payload)
		return
	}

	if monitor.Active {
		ctx := context.Background()
		if err := l.supervisor.StartMonitor(ctx, monitor, false); err != nil {
			fmt.Printf("Failed to start health check for monitor %s: %v\n", monitor.ID, err)
		}
	} else {
		l.supervisor.DeleteMonitor(monitor.ID)
	}
}

// handleMonitorDeleted stops health check polling for deleted monitors
func (l *EventListener) handleMonitorDeleted(event events.Event) {
	monitorID, ok := event.Payload.(string)
	if !ok {
		fmt.Printf("Invalid payload type for monitor.deleted event: %T\n", event.Payload)
		return
	}

	l.supervisor.DeleteMonitor(monitorID)
}

func (l *EventListener) handleProxyUpdated(event events.Event) {
	proxy, ok := event.Payload.(*shared.Proxy)
	if !ok {
		l.supervisor.logger.Warnf("Invalid payload for proxy.updated event: %T", event.Payload)
		return
	}
	ctx := context.Background()
	monitors, err := l.supervisor.monitorSvc.FindByProxyId(ctx, proxy.ID)
	if err != nil {
		l.supervisor.logger.Errorf("Failed to find monitors for proxy %s: %v", proxy.ID, err)
		return
	}
	for _, m := range monitors {
		if err := l.supervisor.StartMonitor(ctx, m, false); err != nil {
			l.supervisor.logger.Errorf("Failed to restart monitor %s for proxy update: %v", m.ID, err)
		}
	}
}

func (l *EventListener) handleProxyDeleted(event events.Event) {
	proxyId, ok := event.Payload.(string)
	if !ok {
		l.supervisor.logger.Warnf("Invalid payload for proxy.deleted event: %T", event.Payload)
		return
	}
	ctx := context.Background()
	monitors, err := l.supervisor.monitorSvc.FindByProxyId(ctx, proxyId)
	if err != nil {
		l.supervisor.logger.Errorf("Failed to find monitors for deleted proxy %s: %v", proxyId, err)
		return
	}
	for _, m := range monitors {
		if err := l.supervisor.StartMonitor(ctx, m, false); err != nil {
			l.supervisor.logger.Errorf("Failed to restart monitor %s for proxy delete: %v", m.ID, err)
		}
	}
}
