package events

import (
	"context"
	"runtime/debug"
	"sync"

	"peekaping/internal/reporting"
)

// subscriber is a named group of handlers with its own queues and worker goroutines.
//
// Every event is queued on the lane of its monitor ID. A lane is handled by at most one
// worker at a time, oldest event first, so one monitor's events keep their order. Workers
// take lanes off a ready list, so a lane whose handler is slow occupies one worker and the
// rest of the workers serve the other lanes. Events with no monitor ID each get a lane of
// their own, so they have no order.
type subscriber struct {
	name string
	bus  *Bus

	mu        sync.Mutex
	hasWork   *sync.Cond // a lane became ready, or the subscriber stopped
	handlers  map[string][]Handler
	lanes     map[string]*lane // lanes with queued or running events, by monitor ID
	ready     queue[*lane]     // lanes with queued events and no worker
	queued    int              // events not started
	running   int              // events being handled
	stopped   bool             // workers exit once no lane is ready, and new events are refused
	aboveMark bool             // a high-water warning is out and the queue hasn't gone down again

	wg sync.WaitGroup // the worker goroutines
}

type lane struct {
	monitorID string
	events    queue[event]
	scheduled bool // on the ready list, or being handled by a worker
}

type event struct {
	topic string
	call  func(context.Context)
}

func newSubscriber(bus *Bus, name string, workers int) *subscriber {
	s := &subscriber{
		name:     name,
		bus:      bus,
		handlers: make(map[string][]Handler),
		lanes:    make(map[string]*lane),
	}
	s.hasWork = sync.NewCond(&s.mu)
	s.wg.Add(workers)
	for range workers {
		go s.work()
	}
	return s
}

func (s *subscriber) addHandler(h Handler) {
	s.mu.Lock()
	s.handlers[h.topic] = append(s.handlers[h.topic], h)
	s.mu.Unlock()
}

// deliver decodes msg for each of the subscriber's handlers for its topic, and queues the
// handlers behind the earlier events of the same monitor. It doesn't wait for them.
func (s *subscriber) deliver(msg Message) {
	s.mu.Lock()
	handlers := append([]Handler(nil), s.handlers[msg.Topic]...)
	s.mu.Unlock()

	for _, h := range handlers {
		monitorID, call, err := h.Bind(msg)
		if err != nil {
			s.bus.logger.Errorw("Failed to decode event, dropping it for this handler",
				"subscriber", s.name,
				"topic", msg.Topic,
				"error", err,
			)
			continue
		}
		s.enqueue(monitorID, event{topic: msg.Topic, call: call})
	}
}

func (s *subscriber) enqueue(monitorID string, ev event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}

	var l *lane
	if monitorID == "" {
		l = &lane{}
	} else if l = s.lanes[monitorID]; l == nil {
		l = &lane{monitorID: monitorID}
		s.lanes[monitorID] = l
	}
	l.events.push(ev)
	s.queued++
	if !l.scheduled {
		l.scheduled = true
		s.ready.push(l)
		s.hasWork.Signal()
	}

	if s.queued >= s.bus.highWater && !s.aboveMark {
		s.aboveMark = true
		s.bus.logger.Warnw("Event queue is past its high-water mark: a handler isn't keeping up",
			"subscriber", s.name,
			"topic", ev.topic,
			"queued", s.queued,
			"high_water", s.bus.highWater,
		)
	}
}

// work is a worker goroutine: it handles one event at a time from the lane at the front of
// the ready list, and puts the lane back at the end if it has more.
func (s *subscriber) work() {
	defer s.wg.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		for s.ready.len() == 0 && !s.stopped {
			s.hasWork.Wait()
		}
		if s.ready.len() == 0 {
			return
		}
		l := s.ready.pop()
		ev := l.events.pop()
		s.queued--
		s.running++

		s.mu.Unlock()
		s.handle(ev)
		s.mu.Lock()

		s.running--
		if l.events.len() > 0 {
			s.ready.push(l)
			s.hasWork.Signal()
		} else {
			l.scheduled = false
			if l.monitorID != "" {
				delete(s.lanes, l.monitorID)
			}
		}
		if s.aboveMark && s.queued < s.bus.highWater/2 {
			s.aboveMark = false
		}
	}
}

// handle runs one handler, recovering from a panic so it can't take down the worker.
func (s *subscriber) handle(ev event) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			s.bus.logger.Errorw("Event handler panicked",
				"subscriber", s.name,
				"topic", ev.topic,
				"panic", r,
				"stack", string(stack),
			)
			reporting.CapturePanic("events", r, stack)
		}
	}()
	ev.call(s.bus.handlerCtx)
}

// stop makes the workers exit once every queued event is handled.
func (s *subscriber) stop() {
	s.mu.Lock()
	s.stopped = true
	s.hasWork.Broadcast()
	s.mu.Unlock()
}

// discard drops the queued events and stops the workers. It returns how many events it
// dropped and how many handlers are still running.
func (s *subscriber) discard() (dropped, running int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dropped = s.queued
	for s.ready.len() > 0 {
		l := s.ready.pop()
		for l.events.len() > 0 {
			l.events.pop()
		}
		l.scheduled = false
	}
	// Lanes with a running handler may hold queued events, too
	for id, l := range s.lanes {
		for l.events.len() > 0 {
			l.events.pop()
		}
		delete(s.lanes, id)
	}
	s.queued = 0
	s.stopped = true
	s.hasWork.Broadcast()
	return dropped, s.running
}

// queue is a first-in, first-out queue.
type queue[T any] struct {
	items []T
	head  int
}

func (q *queue[T]) len() int { return len(q.items) - q.head }

func (q *queue[T]) push(v T) { q.items = append(q.items, v) }

func (q *queue[T]) pop() T {
	v := q.items[q.head]
	var zero T
	q.items[q.head] = zero
	q.head++
	switch {
	case q.head == len(q.items):
		q.items, q.head = q.items[:0], 0
	case q.head >= 64 && q.head*2 >= len(q.items):
		q.items = append(q.items[:0], q.items[q.head:]...)
		q.head = 0
	}
	return v
}
