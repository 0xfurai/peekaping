package probe

import (
	"slices"
	"strings"
	"time"
)

// InstanceOnlineWindow is how long after its last request an instance counts as connected. The
// protocol deals a probe's monitors among the instances inside it, and the sweep tells the
// waiting polls when one leaves it.
const InstanceOnlineWindow = 60 * time.Second

// MaxInstancesPerProbe is the most instance rows one probe holds. A probe process makes a fresh id
// every time it starts, so without a cap a restart loop, or a leaked token used on purpose, fills
// the table faster than the sweep prunes it. A hello from a new instance past the cap takes the
// place of the probe's oldest instance that isn't online.
const MaxInstancesPerProbe = 64

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

// FilterOnline returns the instances that were seen within window of now, sorted by id: the ones
// OnlineInstances names, themselves and not their ids.
func FilterOnline(instances []*Instance, now time.Time, window time.Duration) []*Instance {
	online := make([]*Instance, 0, len(instances))
	for _, instance := range instances {
		if instance != nil && now.Sub(instance.LastSeenAt) <= window {
			online = append(online, instance)
		}
	}
	slices.SortFunc(online, func(a, b *Instance) int { return strings.Compare(a.ID, b.ID) })
	return online
}

// RunsType reports whether a build that lists these supported types can run monitors of the type.
// An empty list means every type: a probe that has not reported its types, such as the local one,
// and an instance of an older probe, list none.
func RunsType(supportedTypes []string, monitorType string) bool {
	return len(supportedTypes) == 0 || slices.Contains(supportedTypes, monitorType)
}

// UnionTypes returns the monitor types that the instances run between them: the types each lists,
// each once, in the order the instances sorted by id list them. An instance that lists none runs
// every type, which makes the union every type, so it is the empty list: the same way a probe says
// that it runs everything. Without instances it is empty too, so a caller that has to tell the two
// apart asks whether there are any.
func UnionTypes(instances []*Instance) []string {
	sorted := slices.Clone(instances)
	slices.SortFunc(sorted, func(a, b *Instance) int { return strings.Compare(a.ID, b.ID) })
	union := []string{}
	for _, instance := range sorted {
		if len(instance.SupportedTypes) == 0 {
			return []string{}
		}
		for _, monitorType := range instance.SupportedTypes {
			if !slices.Contains(union, monitorType) {
				union = append(union, monitorType)
			}
		}
	}
	return union
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
