package protocol

import (
	"context"
	"sync"
	"time"
)

// acceptedTTL is how long the server remembers the ids of the results it took: a result id the
// probe sends again within this time is answered duplicate. It is the age at which a result is
// too old to be stored, so a result that is old enough to be stored again has been forgotten.
const acceptedTTL = time.Hour

// ClaimTTL is how long a window shared between processes holds a claim that is neither kept nor
// released. A request claims its ids, stores the results and then keeps them; a request that dies
// in between (OOM, SIGKILL, eviction of the pod) does neither, and the claim has to run out by
// itself, or the probe's retry would be answered duplicate for the whole hour and the results lost.
// It is longer than a request can run: the probe gives up on a call after 30 seconds
// (probeclient.requestTimeout), which cancels the request's context.
const ClaimTTL = 2 * time.Minute

// Window remembers, for each probe, the ids of the results that were taken in the last hour. A
// probe sends a batch again when it didn't hear the answer, and the window is what keeps that from
// storing the same results twice.
//
// A request claims an id before it stores the result, so that a retry that arrives while the first
// request is still storing finds the id taken. A result that fails to store gives its id back
// (Release), for the probe's retry to be accepted. A result that is stored has its id kept (Keep),
// for the rest of the hour. A claim that is neither released nor kept belongs to a request that
// died, and a window that outlives the process (Redis) forgets it after ClaimTTL.
//
// Which replica a retry reaches is up to the load balancer, however long after the first try, so
// in cluster mode the window is shared by the API replicas: the Redis window. Single mode has one
// process and keeps the window in its memory. internal/app provides the one the mode needs.
// TestProbeProtocol_ARetryOnAnotherReplicaIsAnsweredDuplicate in apitest runs two replicas on one
// Redis and guards it.
type Window interface {
	// Claim takes the probe's result id at now, and reports whether it was free: false when the id
	// is held. A claim holds the id until it is released or kept, and a window shared between
	// processes forgets a claim that is neither after ClaimTTL. Of any number of claims of an id at
	// once, one succeeds. Claiming an id the window still holds leaves the time it was first
	// claimed. An error means the window could not say, and the id is not claimed.
	Claim(ctx context.Context, probeID, id string, now time.Time) (bool, error)
	// Keep marks ids that were claimed for results that were stored: the window holds them for the
	// hour from now, whatever happens to the claims' own time. It is called as soon as the results
	// are stored, with all of one batch's ids at once, and finishes even if the request was
	// cancelled. It sets the hour on an id whose claim ran out meanwhile too, since the result is
	// stored all the same. An error means the ids may be forgotten after ClaimTTL.
	Keep(ctx context.Context, probeID string, ids ...string) error
	// Release gives back an id that was claimed for a result that was not stored, so that the
	// probe's next attempt at it is accepted. It is called when the request is ending, and
	// finishes even if the request was cancelled.
	Release(ctx context.Context, probeID, id string) error
}

// NewMemoryWindow is single mode's window: it lives in the memory of the one process, and a
// restart forgets it
func NewMemoryWindow() Window { return newAcceptedWindow(acceptedTTL) }

// acceptedWindow is the window in the memory of one process. In cluster mode it would give each
// API replica a window of its own, so a retry that lands on another replica would not be found.
type acceptedWindow struct {
	ttl        time.Duration
	sweepEvery time.Duration

	mu        sync.Mutex
	probes    map[string]map[string]time.Time
	lastSweep time.Time
}

var _ Window = (*acceptedWindow)(nil)

func newAcceptedWindow(ttl time.Duration) *acceptedWindow {
	return &acceptedWindow{
		ttl: ttl,
		// A probe with a busy monitor list adds hundreds of ids a second, so forgetting the
		// expired ones on every add would walk the whole window each time
		sweepEvery: max(ttl/60, time.Nanosecond),
		probes:     map[string]map[string]time.Time{},
	}
}

// Claim implements Window
func (w *acceptedWindow) Claim(_ context.Context, probeID, id string, now time.Time) (bool, error) {
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
		return false, nil
	}
	ids[id] = now
	return true, nil
}

// Keep implements Window. A claim in memory is held for the whole hour from the start: there is
// no shorter time for it to run out in, as the memory goes with the process that died.
func (w *acceptedWindow) Keep(context.Context, string, ...string) error { return nil }

// Release implements Window
func (w *acceptedWindow) Release(_ context.Context, probeID, id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	ids := w.probes[probeID]
	delete(ids, id)
	if len(ids) == 0 {
		delete(w.probes, probeID)
	}
	return nil
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
