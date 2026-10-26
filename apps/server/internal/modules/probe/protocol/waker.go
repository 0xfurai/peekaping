package protocol

import (
	"context"
	"sync"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/shared"

	"go.uber.org/zap"
)

// wakerSubscriber names the waker's subscriber on the event bus
const wakerSubscriber = "probe-protocol"

// Waker ends the assignment polls that are waiting when something they would answer changes. One
// runs in each API process, and the event bus carries a change that another process made to it.
//
// A monitor created, updated or deleted wakes the waiters of every probe: telling which probes a
// monitor belongs to is the work of the poll that wakes, one query for each probe. A monitor moved
// from one probe to another is in the event only as its new probe's, and the probe it left has to
// hear of it as well, so every probe wakes. An InstancesChanged event wakes the waiters of that
// probe alone.
//
// An event is only a notice. The poll that wakes reads the database and answers if its version
// differs, so a wake that changes nothing for it, and a wake that arrives twice, are harmless.
type Waker struct {
	bus    events.EventBus
	logger *zap.SugaredLogger

	mu      sync.Mutex
	waiters map[string]map[*waiter]struct{} // by probe id

	closed    chan struct{}
	closeOnce sync.Once
}

// waiter is one Wait. Its fields are guarded by the Waker's mu.
type waiter struct {
	ch   chan struct{}
	done bool
	// stop detaches the waiter from its context once it has ended another way
	stop func() bool
}

// NewWaker returns a waker for the processes on bus. Subscribe starts it listening.
func NewWaker(bus events.EventBus, logger *zap.SugaredLogger) *Waker {
	return &Waker{
		bus:     bus,
		logger:  logger.Named("[probe-waker]"),
		waiters: make(map[string]map[*waiter]struct{}),
		closed:  make(chan struct{}),
	}
}

// Close ends every wait and tells the polls that the server is stopping, through Closed, so that
// they answer instead of holding the HTTP server's shutdown for up to PollTimeout. It may be
// called more than once.
func (w *Waker) Close() {
	w.closeOnce.Do(func() { close(w.closed) })
	w.wakeAll("shutdown")
}

// Closed is closed once Close has been called
func (w *Waker) Closed() <-chan struct{} { return w.closed }

// Subscribe has the waker listen for the events that end a poll. Call it at startup, before the
// server takes requests, as the other listeners are: an event handed to a bus with no handler for
// its topic is dropped.
func (w *Waker) Subscribe() {
	subscriber := events.NewSubscriber(w.bus, wakerSubscriber)
	events.Subscribe(subscriber, events.MonitorCreated, func(context.Context, shared.Monitor) { w.wakeAll("monitor created") })
	events.Subscribe(subscriber, events.MonitorUpdated, func(context.Context, shared.Monitor) { w.wakeAll("monitor updated") })
	events.Subscribe(subscriber, events.MonitorDeleted, func(context.Context, string) { w.wakeAll("monitor deleted") })
	events.Subscribe(subscriber, probe.InstancesChanged, func(_ context.Context, e probe.InstancesChangedEvent) { w.wake(e.ProbeID) })
}

// Wait returns a channel that is closed on the next wake of the probe, or when ctx is done,
// whichever comes first. A poll that checks whether anything changed has to call it before it
// reads the database: a change that lands between the read and the call would be missed, and
// the poll would sleep through it.
func (w *Waker) Wait(ctx context.Context, probeID string) <-chan struct{} {
	wt := &waiter{ch: make(chan struct{})}
	w.mu.Lock()
	if w.waiters[probeID] == nil {
		w.waiters[probeID] = make(map[*waiter]struct{})
	}
	w.waiters[probeID][wt] = struct{}{}
	w.mu.Unlock()

	stop := context.AfterFunc(ctx, func() { w.forget(probeID, wt) })

	w.mu.Lock()
	if wt.done {
		// A wake or the context ended it before the line above got to run
		stop()
	} else {
		wt.stop = stop
	}
	w.mu.Unlock()
	return wt.ch
}

// forget ends the wait of a context that is done
func (w *Waker) forget(probeID string, wt *waiter) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if set := w.waiters[probeID]; set != nil {
		delete(set, wt)
		if len(set) == 0 {
			delete(w.waiters, probeID)
		}
	}
	wt.end()
}

// wake ends every wait of the probe
func (w *Waker) wake(probeID string) {
	w.mu.Lock()
	set := w.waiters[probeID]
	delete(w.waiters, probeID)
	for wt := range set {
		wt.end()
	}
	w.mu.Unlock()

	if len(set) > 0 {
		w.logger.Debugw("Woke the assignment polls of a probe", "probeId", probeID, "polls", len(set), "reason", "instances changed")
	}
}

// wakeAll ends every wait of every probe
func (w *Waker) wakeAll(reason string) {
	w.mu.Lock()
	polls := 0
	for _, set := range w.waiters {
		for wt := range set {
			wt.end()
			polls++
		}
	}
	clear(w.waiters)
	w.mu.Unlock()

	if polls > 0 {
		w.logger.Debugw("Woke the assignment polls of every probe", "polls", polls, "reason", reason)
	}
}

// end closes the waiter's channel, once. The caller holds the Waker's mu.
func (wt *waiter) end() {
	if wt.done {
		return
	}
	wt.done = true
	close(wt.ch)
	if wt.stop != nil {
		wt.stop()
	}
}
