// Package events is the event bus that connects the parts of Peekaping, in one process or
// across processes.
//
// A Topic[T] names an event and its payload type. Publish and Subscribe are generic
// functions over an EventBus:
//
//	events.Publish(ctx, bus, events.MonitorDeleted, monitorID)
//
//	subscriber := events.NewSubscriber(bus, "producer")
//	events.Subscribe(subscriber, events.MonitorDeleted, func(ctx context.Context, monitorID string) { ... })
//
// A subscriber is a named group of handlers that shares queues. It handles one monitor's
// events in the order they were published, across all its topics, and different monitors
// at the same time, up to a limit of workers. A slow or panicking handler never holds up
// Publish or another subscriber, and holds up another monitor only by occupying one of the
// subscriber's workers. Bus implements all of that over a Transport, which only has to carry
// messages: the Redis bus in package infra is Bus over a Redis transport.
//
// Handlers must not change the payload they are given when it holds a slice, map or
// pointer: an in-process transport hands every handler the same value.
package events

import "peekaping/internal/modules/shared"

// The topics whose payloads are types in package shared. A topic with a payload type of its
// own lives beside that type: certificate.CertificateExpiry, stale.MonitorStale and
// stale.MonitorStaleRecovered.
//
// A topic's name is its name on the wire, so keep the names as they are.
var (
	// MonitorCreated is published when a monitor is created
	MonitorCreated = NewTopic("monitor.created", monitorOf)
	// MonitorUpdated is published when a monitor is updated
	MonitorUpdated = NewTopic("monitor.updated", monitorOf)
	// MonitorDeleted is published when a monitor is deleted. Its payload is the monitor's ID.
	MonitorDeleted = NewTopic("monitor.deleted", func(monitorID string) string { return monitorID })
	// HeartbeatEvent is published when a heartbeat is created
	HeartbeatEvent = NewTopic("heartbeat", heartbeatOf)
	// MonitorStatusChanged is published when a heartbeat moves its monitor to a different status
	MonitorStatusChanged = NewTopic("monitor.status.changed", heartbeatOf)
	// ImportantHeartbeat is published when a heartbeat is important for notification purposes
	ImportantHeartbeat = NewTopic("important.heartbeat", heartbeatOf)
)

func monitorOf(m shared.Monitor) string { return m.ID }

func heartbeatOf(h shared.HeartBeatModel) string { return h.MonitorID }
