package protocol

import (
	"hash/fnv"
	"slices"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
)

// pushType is the monitor type that runs no checks of its own, so no probe is dealt one
const pushType = "push"

// Dealable returns the monitors a probe can be dealt, in the order given: the active ones, never
// a push monitor (it waits for pushes and checks nothing), and, when supportedTypes lists
// anything, only the types the probe can run. An empty list of supported types means every type:
// it is what a probe that has not reported its types, such as the local one, has.
//
// It returns an empty list, not nil, when there are none.
func Dealable(monitors []*monitor.Model, supportedTypes []string) []*monitor.Model {
	dealable := make([]*monitor.Model, 0, len(monitors))
	for _, m := range monitors {
		if m == nil || !m.Active || m.Type == pushType || !probe.RunsType(supportedTypes, m.Type) {
			continue
		}
		dealable = append(dealable, m)
	}
	return dealable
}

// Deal returns the monitors that instanceID runs among the instances. Every monitor goes to
// exactly one of the instances that run its type (their supported types include it, or are
// empty): the one whose hash with the monitor is the highest (rendezvous hashing). So an instance
// that joins takes a share from each of the others that run what it takes, and moves no monitor
// between them, and one that leaves gives its monitors to the others that can run them and moves
// none of theirs. A monitor that no instance runs is dealt to none.
//
// It depends on the instances' ids and types alone, not on the order of the instances or the
// monitors, so every API replica deals the same. It returns an empty list when instanceID is not
// among the instances.
func Deal(monitors []*monitor.Model, instances []*probe.Instance, instanceID string) []*monitor.Model {
	share := make([]*monitor.Model, 0, len(monitors)/max(len(instances), 1))
	if !slices.ContainsFunc(instances, func(i *probe.Instance) bool { return i.ID == instanceID }) {
		return share
	}
	// The ids of the instances that run a type, worked out once for each type
	runners := map[string][]string{}
	for _, m := range monitors {
		ids, known := runners[m.Type]
		if !known {
			for _, instance := range instances {
				if probe.RunsType(instance.SupportedTypes, m.Type) {
					ids = append(ids, instance.ID)
				}
			}
			runners[m.Type] = ids
		}
		if len(ids) > 0 && owner(m.ID, ids) == instanceID {
			share = append(share, m)
		}
	}
	return share
}

// owner is the instance with the highest weight for the monitor. Equal weights, which two
// different ids reach only by a collision, go to the smaller id so that the choice is still
// the same everywhere.
func owner(monitorID string, instanceIDs []string) string {
	var best string
	var bestWeight uint64
	for i, id := range instanceIDs {
		weight := weigh(monitorID, id)
		if i == 0 || weight > bestWeight || (weight == bestWeight && id < best) {
			best, bestWeight = id, weight
		}
	}
	return best
}

// weigh is the FNV-1a hash of the monitor's id, a separator no id holds, and the instance's id.
// The bits FNV-1a produces are poorly mixed: for instance ids that differ only in their last
// characters, such as edge-1 and edge-2, the winner is decided by the lowest bits of the hash, and
// one instance of four took half of the monitors in a trial. mix spreads every bit over the rest.
func weigh(monitorID, instanceID string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(monitorID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(instanceID))
	return mix(h.Sum64())
}

// mix is the finalizer of MurmurHash3's 64-bit hash: a change in any bit of x changes each bit of
// the result with about even odds
func mix(x uint64) uint64 {
	x ^= x >> 33
	x *= 0xff51afd7ed558ccd
	x ^= x >> 33
	x *= 0xc4ceb9fe1a85ec53
	x ^= x >> 33
	return x
}
