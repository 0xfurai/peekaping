package protocol

import (
	"sync"
	"time"
)

// acceptedTTL is how long the server remembers the ids of the results it stored: a result id the
// probe sends again within this time is answered duplicate. It is the age at which a result is
// too old to be stored, so a result that is old enough to be stored again has been forgotten.
const acceptedTTL = time.Hour

// acceptedWindow remembers, for each probe, the ids of the results that were stored in the last
// ttl. A probe sends a batch again when it didn't hear the answer, and the window is what keeps
// that from storing the same results twice.
//
// It lives in the memory of one process. In cluster mode the API replicas each have their own, so
// a retry that lands on another replica is not found here, and the push queue's task id catches
// it instead: it is the monitor id and the start time, kept for the task's retention (an hour),
// and the route answers a result whose task the queue holds already duplicate. A restart forgets
// the window, which the queue's task ids cover in cluster mode and nothing covers in single mode:
// a retry sent in the seconds around a restart can store a result twice.
type acceptedWindow struct {
	ttl        time.Duration
	sweepEvery time.Duration

	mu        sync.Mutex
	probes    map[string]map[string]time.Time
	lastSweep time.Time
}

func newAcceptedWindow(ttl time.Duration) *acceptedWindow {
	return &acceptedWindow{
		ttl: ttl,
		// A probe with a busy monitor list adds hundreds of ids a second, so forgetting the
		// expired ones on every add would walk the whole window each time
		sweepEvery: max(ttl/60, time.Nanosecond),
		probes:     map[string]map[string]time.Time{},
	}
}

// Seen reports whether the probe's result id was added less than the window's ttl before now
func (w *acceptedWindow) Seen(probeID, id string, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	added, ok := w.probes[probeID][id]
	return ok && now.Sub(added) < w.ttl
}

// Add remembers that the probe's result id was stored at now. Adding an id the window still holds
// leaves the time it was first added.
func (w *acceptedWindow) Add(probeID, id string, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if now.Sub(w.lastSweep) >= w.sweepEvery {
		w.sweep(now)
	}
	ids := w.probes[probeID]
	if ids == nil {
		ids = map[string]time.Time{}
		w.probes[probeID] = ids
	}
	if added, ok := ids[id]; ok && now.Sub(added) < w.ttl {
		return
	}
	ids[id] = now
}

// sweep forgets the ids that have expired, and the probes that are left with none
func (w *acceptedWindow) sweep(now time.Time) {
	for probeID, ids := range w.probes {
		for id, added := range ids {
			if now.Sub(added) >= w.ttl {
				delete(ids, id)
			}
		}
		if len(ids) == 0 {
			delete(w.probes, probeID)
		}
	}
	w.lastSweep = now
}

// probeCount is the number of probes the window holds ids for
func (w *acceptedWindow) probeCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.probes)
}

// idCount is the number of ids the window holds, expired and not
func (w *acceptedWindow) idCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, ids := range w.probes {
		n += len(ids)
	}
	return n
}
