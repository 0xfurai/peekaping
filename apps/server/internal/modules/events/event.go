package events

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

// EventBus defines the interface for event bus implementations
// This allows different implementations (Redis, Kafka, RabbitMQ, etc.)
type EventBus interface {
	// Subscribe registers a handler for a specific event type
	Subscribe(eventType EventType, handler EventHandler)

	// Publish sends an event to all registered handlers
	Publish(event Event)

	// Close closes the event bus and cleans up resources
	Close() error
}

// HeartbeatCreatedPayload represents the payload for heartbeat created events
type HeartbeatCreatedPayload struct {
	MonitorID string
	Status    int
	Ping      int
	Time      int64 // Unix seconds
}
