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

// RetiredAt is the last seen time an instance is left with when it says bye at now: a window and a
// second ago. An instance seen exactly a window ago still counts as online (OnlineInstances is
// inclusive), so the extra second is what takes the instance out of the dealing at once, even
// when the next request reads the same clock instant or the database rounds the stored time. The
// probe's offline threshold counts from this time, a window and a second early.
//
// That backdate is why PROBE_OFFLINE_AFTER has a floor (MinOfflineAfter): the time a restarting
// probe has to say hello again is the limit less the backdate, and the sweep sets the probe
// offline, with a notification, once the limit has passed. With a limit under about 62 s plus a
// sweep pass, even a graceful restart would raise one.
func RetiredAt(now time.Time) time.Time { return now.Add(-InstanceOnlineWindow - time.Second) }
