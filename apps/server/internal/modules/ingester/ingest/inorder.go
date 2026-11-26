package ingest

import (
	"context"
	"slices"
	"sync"
)

// inOrderService ingests one monitor's results one at a time, in the order they were submitted
type inOrderService struct {
	inner Service

	mu sync.Mutex
	// monitors has an entry for each monitor with an Ingest running or waiting, and none
	// for the rest, so deleted monitors leave nothing behind
	monitors map[string]*monitorQueue
}

// monitorQueue holds the Ingest calls waiting behind the running one, oldest first. Each
// is waiting for its channel to close.
type monitorQueue struct {
	waiting []chan struct{}
}

// NewInOrderService wraps a Service so that the Ingest calls for one monitor run one at a
// time, in the order they were made. Ingest reads the monitor's previous heartbeat, so two
// results of one monitor ingested at once would both read the same one. The results of
// different monitors are ingested at the same time.
//
// Checks and pushes of a monitor must go through the same wrapper to be ordered with each
// other. Wrapping a Service that is already in order returns it.
func NewInOrderService(inner Service) Service {
	if ordered, ok := inner.(*inOrderService); ok {
		return ordered
	}
	return &inOrderService{inner: inner, monitors: make(map[string]*monitorQueue)}
}

// Ingest waits for the monitor's earlier results to be ingested, then ingests this one. A
// result still waiting when ctx is done is dropped and ctx's error returned.
func (s *inOrderService) Ingest(ctx context.Context, r *Result) error {
	if err := s.enter(ctx, r.MonitorID); err != nil {
		return err
	}
	defer s.leave(r.MonitorID)
	return s.inner.Ingest(ctx, r)
}

// enter returns once it is the Ingest call's turn for the monitor
func (s *inOrderService) enter(ctx context.Context, monitorID string) error {
	s.mu.Lock()
	queue, busy := s.monitors[monitorID]
	if !busy {
		s.monitors[monitorID] = &monitorQueue{}
		s.mu.Unlock()
		return nil
	}
	turn := make(chan struct{})
	queue.waiting = append(queue.waiting, turn)
	s.mu.Unlock()

	select {
	case <-turn:
		return nil
	case <-ctx.Done():
	}

	s.mu.Lock()
	if i := slices.Index(queue.waiting, turn); i >= 0 {
		queue.waiting = slices.Delete(queue.waiting, i, i+1)
		s.mu.Unlock()
		return ctx.Err()
	}
	s.mu.Unlock()
	// The turn came as ctx ended. The call owns it, so it passes it on.
	s.leave(monitorID)
	return ctx.Err()
}

// leave ends the running Ingest call's turn and hands it to the next waiting one
func (s *inOrderService) leave(monitorID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	queue := s.monitors[monitorID]
	if len(queue.waiting) == 0 {
		delete(s.monitors, monitorID)
		return
	}
	next := queue.waiting[0]
	queue.waiting[0] = nil
	queue.waiting = queue.waiting[1:]
	close(next)
}
