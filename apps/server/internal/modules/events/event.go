package events

import (
	"sync"

	"go.uber.org/zap"
)

// EventType represents the type of event
type EventType string

const (
	// MonitorCreated is emitted when a monitor is created
	MonitorCreated EventType = "monitor.created"
	// MonitorUpdated is emitted when a monitor is updated
	MonitorUpdated EventType = "monitor.updated"
	// MonitorDeleted is emitted when a monitor is deleted
	MonitorDeleted EventType = "monitor.deleted"
	// HeartbeatEvent is emitted when a heartbeat is created
	HeartbeatEvent EventType = "heartbeat"
	// NotifyEvent is emitted when a monitor status changes (up <-> down)
	MonitorStatusChanged EventType = "monitor.status.changed"
	// ProxyUpdated is emitted when a proxy is updated
	ProxyUpdated EventType = "proxy.updated"
	// ProxyDeleted is emitted when a proxy is deleted
	ProxyDeleted EventType = "proxy.deleted"
	// CertificateExpiry is emitted when a certificate is expiring
	CertificateExpiry EventType = "certificate.expiry"
	// ImportantHeartbeat is emitted when a heartbeat is important for notification purposes
	ImportantHeartbeat EventType = "important.heartbeat"
)

// Event represents a generic event with a type and payload
type Event struct {
	Type    EventType
	Payload interface{}
}

// EventHandler is a function that handles events
type EventHandler func(event Event)

// EventBus manages event subscriptions and publishing
type EventBus struct {
	mu       sync.RWMutex
	handlers map[EventType][]EventHandler
	logger   *zap.SugaredLogger
}

// NewEventBus creates a new event bus
func NewEventBus(logger *zap.SugaredLogger) *EventBus {
	return &EventBus{
		handlers: make(map[EventType][]EventHandler),
		logger:   logger,
	}
}

// Subscribe registers a handler for a specific event type
func (b *EventBus) Subscribe(eventType EventType, handler EventHandler) {
	b.logger.Debugf("Subscribing to event: %s", eventType)
	b.mu.Lock()
	defer b.mu.Unlock()

	handlers := b.handlers[eventType]
	handlers = append(handlers, handler)
	b.handlers[eventType] = handlers
}

// Publish sends an event to all registered handlers
func (b *EventBus) Publish(event Event) {
	b.logger.Debugf("Publishing event: %s", event.Type)
	b.mu.RLock()
	defer b.mu.RUnlock()

	handlers := b.handlers[event.Type]
	for _, handler := range handlers {
		go handler(event)
	}
}

type HeartbeatCreatedPayload struct {
	MonitorID string
	Status    int
	Ping      int
	Time      int64 // Unix seconds
}
