package probe

import (
	"time"

	"peekaping/internal/modules/events"
)

// Event is the payload of the ProbeOffline and ProbeOnline topics
type Event struct {
	ProbeID   string `json:"probe_id"`
	ProbeName string `json:"probe_name"`
	// Since is the last time any instance of the probe was seen, for ProbeOffline (the time the
	// probe was found to have none, for a local probe that never had one), and the time of the
	// request that brought the probe back, for ProbeOnline
	Since time.Time `json:"since"`
}

// The topics of Event. The sweep and the probe protocol's bearer middleware publish them, and
// the notification channels listen. Events of one probe are handled in the order they were
// published: the probe's ID takes the place of the monitor's ID as the key that orders them,
// which cannot collide with a monitor's.
//
// The names are the topics' names on the wire, so keep them as they are.
var (
	// ProbeOffline is published when the sweep finds that none of an online probe's instances was
	// seen within PROBE_OFFLINE_AFTER
	ProbeOffline = events.NewTopic("probe.offline", func(e Event) string { return e.ProbeID })
	// ProbeOnline is published when an offline probe is heard from again
	ProbeOnline = events.NewTopic("probe.online", func(e Event) string { return e.ProbeID })
)

// InstancesChangedEvent is the payload of InstancesChanged
type InstancesChangedEvent struct {
	ProbeID string `json:"probe_id"`
}

// InstancesChanged is published when the instances a probe is dealt among change: one said hello
// after being out of sight, one said bye, or the sweep dropped one that went quiet. The API
// replicas wake the probe's waiting assignment polls, whose instances are dealt a new share. It
// belongs to no monitor, so it is handled with no order relative to other events.
//
// The name is the topic's name on the wire, so keep it as it is.
var InstancesChanged = events.NewTopic("probe.instances.changed", func(InstancesChangedEvent) string { return "" })
