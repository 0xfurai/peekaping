package probe

import (
	"slices"
	"time"
)

// InstanceOnlineWindow is how long after its last request an instance counts as connected. The
// protocol deals a probe's monitors among the instances inside it, and the sweep tells the
// waiting polls when one leaves it.
const InstanceOnlineWindow = 60 * time.Second

// OnlineInstances returns the ids of the instances that were seen within window of now, sorted.
// An instance seen by a clock a little ahead of ours counts as seen just now.
func OnlineInstances(instances []*Instance, now time.Time, window time.Duration) []string {
	online := make([]string, 0, len(instances))
	for _, instance := range instances {
		if instance != nil && now.Sub(instance.LastSeenAt) <= window {
			online = append(online, instance.ID)
		}
	}
	slices.Sort(online)
	return online
}
