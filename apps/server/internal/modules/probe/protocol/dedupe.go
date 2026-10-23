package protocol

import (
	"sync"
	"time"
)

// acceptedTTL is how long the server remembers the ids of the results it took: a result id the
// probe sends again within this time is answered duplicate. It is the age at which a result is
// too old to be stored, so a result that is old enough to be stored again has been forgotten.
const acceptedTTL = time.Hour

// acceptedWindow remembers, for each probe, the ids of the results that were taken in the last
// ttl. A probe sends a batch again when it didn't hear the answer, and the window is what keeps
// that from storing the same results twice.
//
// A request claims an id before it stores the result, so that a retry that arrives while the first
// request is still storing finds the id taken. A result that fails to store gives its id back
// (Release), for the probe's retry to be accepted.
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

// Claim takes the probe's result id at now, and reports whether it was free: false when the id was
// claimed less than the window's ttl before now. Of any number of claims of an id at once, one
// succeeds. Claiming an id the window still holds leaves the time it was first claimed.
func (w *acceptedWindow) Claim(probeID, id string, now time.Time) bool {
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
	if claimed, ok := ids[id]; ok && now.Sub(claimed) < w.ttl {
		return false
	}
	ids[id] = now
	return true
}

// Release gives back an id that was claimed for a result that was not stored, so that the probe's
// next attempt at it is accepted
func (w *acceptedWindow) Release(probeID, id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	ids := w.probes[probeID]
	delete(ids, id)
	if len(ids) == 0 {
		delete(w.probes, probeID)
	}
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
