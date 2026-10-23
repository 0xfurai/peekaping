package probe

import "peekaping/internal/modules/events"

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
