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
