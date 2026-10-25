package scheduler

import (
	"context"
	"sync"
	"sync/atomic"

	"peekaping/internal/modules/check/job"
	"peekaping/internal/modules/shared"
)

// pushType is the monitor type whose check is the wait for a push
const pushType = "push"

// PushSource is a Source restricted to the push monitors of another Source. A push monitor runs no
// check of its own: the scheduler's job for it is to find that no push arrived in time and ingest a
// Down. In the cluster on SQLite and PostgreSQL no process of Peekaping's own checks anything else,
// since the probes run the checks, so the api replica that holds the leader lock runs a scheduler on
// this source for that one job.
//
// Its Watch subscribes to the inner source once, however many schedulers use it in turn, as a
// replica does when it holds the lock, loses it and gets it back: the signals go to the callbacks
// of the scheduler that watches now, and to none between two terms.
type PushSource struct {
	inner Source

	mu sync.Mutex
	// pushes are the push monitors the last listing found, which the pages are cut from
	pushes []*shared.Monitor

	watchOnce sync.Once
	watching  atomic.Pointer[watchers]
}

// watchers are the callbacks of the scheduler that watches the source
type watchers struct {
	created, updated, deleted func(monitorID string)
}

var _ Source = (*PushSource)(nil)

// NewPushSource restricts the source to push monitors
func NewPushSource(inner Source) *PushSource { return &PushSource{inner: inner} }

// ListActive pages the active push monitors of the inner source. The inner source's pages hold
// monitors of every type, so the first page reads them all and the later pages are cut from what
// that found: the scheduler reads the pages in turn, with its reconcile holding off other reads.
func (p *PushSource) ListActive(ctx context.Context, page int, limit int) ([]*shared.Monitor, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if page == 0 {
		found, err := p.scan(ctx, limit)
		if err != nil {
			return nil, err
		}
		p.pushes = found
	}
	from := min(page*limit, len(p.pushes))
	to := min(from+limit, len(p.pushes))
	return p.pushes[from:to], nil
}

// scan reads every active monitor of the inner source and keeps the push monitors
func (p *PushSource) scan(ctx context.Context, limit int) ([]*shared.Monitor, error) {
	var pushes []*shared.Monitor
	for page := 0; ; page++ {
		monitors, err := p.inner.ListActive(ctx, page, limit)
		if err != nil {
			return nil, err
		}
		for _, mon := range monitors {
			if mon != nil && mon.Type == pushType {
				pushes = append(pushes, mon)
			}
		}
		if len(monitors) < limit {
			return pushes, nil
		}
	}
}

// Find returns the monitor if it is a push monitor of the inner source
func (p *PushSource) Find(ctx context.Context, monitorID string) (*shared.Monitor, error) {
	mon, err := p.inner.Find(ctx, monitorID)
	if err != nil || mon == nil || mon.Type != pushType {
		return nil, err
	}
	return mon, nil
}

// Load loads the check of the monitor
func (p *PushSource) Load(ctx context.Context, monitorID string) (*job.Job, error) {
	return p.inner.Load(ctx, monitorID)
}

// Watch has the callbacks called on the signals of the inner source, in place of the callbacks of
// the scheduler that watched before. A signal for a monitor that isn't a push monitor reaches them
// too: they read the monitor with Find, which answers nil for it, and drop it.
func (p *PushSource) Watch(created, updated, deleted func(monitorID string)) {
	p.watching.Store(&watchers{created: created, updated: updated, deleted: deleted})
	p.watchOnce.Do(func() {
		p.inner.Watch(
			func(id string) {
				if w := p.watching.Load(); w != nil {
					w.created(id)
				}
			},
			func(id string) {
				if w := p.watching.Load(); w != nil {
					w.updated(id)
				}
			},
			func(id string) {
				if w := p.watching.Load(); w != nil {
					w.deleted(id)
				}
			},
		)
	})
}

// StopWatching drops the callbacks, for the time between two terms
func (p *PushSource) StopWatching() { p.watching.Store(nil) }
