package probeclient

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"time"

	"go.uber.org/zap"

	"peekaping/internal/modules/check/job"
	"peekaping/internal/modules/shared"
	"peekaping/internal/probeproto"
)

// Source is the assignment a probe follows, as the scheduler reads its monitors: it implements
// scheduler.Source. Run keeps the assignment up to date over the server's long poll, and tells
// the scheduler what changed. The assignment it holds is the last one the server sent, whatever
// has happened since: while the server can't be reached the probe keeps checking on it.
//
// Before the first assignment arrives the source lists nothing.
type Source struct {
	session *Session
	logger  *zap.SugaredLogger

	mu sync.RWMutex
	// version is the version of the assignment held; empty before the first
	version string
	// received is set once an assignment has been taken
	received bool
	// monitors is the assignment by monitor id, and ids the same ids in order
	monitors map[string]probeproto.Monitor
	ids      []string
	watchers []watcher
}

// watcher is what a Watch call asked to be told
type watcher struct {
	created, updated, deleted func(monitorID string)
}

// NewSource creates a source that follows the assignment over the session. It makes no call: Run does.
func NewSource(session *Session) *Source {
	return &Source{
		session:  session,
		logger:   session.loggerOf("probe_source"),
		monitors: map[string]probeproto.Monitor{},
	}
}

// Ready reports whether the probe has received its first assignment, even an empty one. It stays
// true while the server can't be reached.
func (s *Source) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.received
}

// Run follows the assignment until ctx ends: it says hello, then polls the server with the version
// it holds, taking each assignment the server answers with. A call that fails is dealt with by the
// session's rules, and the poll goes on from the assignment held.
func (s *Source) Run(ctx context.Context) {
	var backoff Backoff
	for ctx.Err() == nil {
		if err := s.session.Connect(ctx); err != nil {
			return
		}
		assignment, err := s.session.poll(ctx, s.heldVersion())
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if err := s.session.wait(ctx, s.session.failed("poll for assignments", err, &backoff)); err != nil {
				return
			}
			continue
		}
		backoff.Reset()
		if assignment != nil {
			s.take(assignment)
		}
	}
}

func (s *Source) heldVersion() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

// take makes the assignment the one held, and tells the watchers what it changes: the monitors it
// no longer lists are deleted, the ones it changes are updated, and the ones it adds are created.
func (s *Source) take(assignment *probeproto.AssignmentsResponse) {
	next := make(map[string]probeproto.Monitor, len(assignment.Monitors))
	for _, m := range assignment.Monitors {
		next[m.ID] = m
	}

	s.mu.Lock()
	previous := s.monitors
	s.monitors = next
	s.ids = sortedKeys(next)
	s.version = assignment.Version
	s.received = true
	watchers := slices.Clone(s.watchers)
	s.mu.Unlock()

	var created, updated, deleted []string
	for _, id := range sortedKeys(next) {
		before, known := previous[id]
		switch {
		case !known:
			created = append(created, id)
		case !sameMonitor(before, next[id]):
			updated = append(updated, id)
		}
	}
	for _, id := range sortedKeys(previous) {
		if _, kept := next[id]; !kept {
			deleted = append(deleted, id)
		}
	}
	if len(created)+len(updated)+len(deleted) == 0 {
		s.logger.Debugw("Assignment version changed, monitors did not", "version", assignment.Version, "monitors", len(next))
		return
	}
	s.logger.Infow("Assignment changed",
		"version", assignment.Version,
		"monitors", len(next),
		"created", len(created),
		"updated", len(updated),
		"deleted", len(deleted))

	for _, w := range watchers {
		notify(w.deleted, deleted)
		notify(w.updated, updated)
		notify(w.created, created)
	}
}

func notify(call func(monitorID string), ids []string) {
	if call == nil {
		return
	}
	for _, id := range ids {
		call(id)
	}
}

func sortedKeys(m map[string]probeproto.Monitor) []string {
	keys := make([]string, 0, len(m))
	for id := range m {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	return keys
}

// sameMonitor reports whether two versions of a monitor are the same as far as checking it goes:
// every field of the monitor and of its proxy but updated_at. A monitor's updated_at moves when
// its status flips, which the probe has no use for, so by itself it is not a change.
func sameMonitor(a, b probeproto.Monitor) bool {
	return reflect.DeepEqual(withoutUpdatedAt(a), withoutUpdatedAt(b))
}

func withoutUpdatedAt(m probeproto.Monitor) probeproto.Monitor {
	m.UpdatedAt = time.Time{}
	if m.Proxy != nil {
		proxy := *m.Proxy
		proxy.UpdatedAt = time.Time{}
		m.Proxy = &proxy
	}
	return m
}

// Watch calls the functions when the assignment creates, changes or drops a monitor. A call is a
// notice, as the scheduler takes it: it reads the monitor with Find. The calls are made on the
// goroutine of Run, after the assignment they follow from is held.
func (s *Source) Watch(created, updated, deleted func(monitorID string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.watchers = append(s.watchers, watcher{created: created, updated: updated, deleted: deleted})
}

// ListActive returns a page of the assigned monitors, by id. The assignment holds active monitors only.
func (s *Source) ListActive(_ context.Context, page int, limit int) ([]*shared.Monitor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if page < 0 || limit <= 0 || page > len(s.ids)/limit {
		return nil, nil
	}
	ids := s.ids[page*limit:]
	ids = ids[:min(limit, len(ids))]
	monitors := make([]*shared.Monitor, len(ids))
	for i, id := range ids {
		monitors[i] = monitorOf(s.monitors[id])
	}
	return monitors, nil
}

// Find returns the assigned monitor, or nil when the probe is not assigned it
func (s *Source) Find(_ context.Context, monitorID string) (*shared.Monitor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, assigned := s.monitors[monitorID]
	if !assigned {
		return nil, nil
	}
	return monitorOf(m), nil
}

// Load returns what a check of the assigned monitor needs. Whether the monitor is under
// maintenance is for the server to judge when it takes the result, so a job is never under it.
func (s *Source) Load(_ context.Context, monitorID string) (*job.Job, error) {
	s.mu.RLock()
	m, assigned := s.monitors[monitorID]
	s.mu.RUnlock()
	if !assigned {
		return nil, job.ErrMonitorNotFound
	}

	certExpiry, err := job.CertExpiryRequested(m.Config)
	if err != nil {
		s.logger.Warnw("Failed to parse monitor config for certificate expiry check",
			"monitor_id", m.ID, "monitor_name", m.Name, "error", err)
	}
	return &job.Job{Monitor: monitorOf(m), Proxy: proxyOf(m.Proxy), CheckCertExpiry: certExpiry}, nil
}

// monitorOf is the monitor as the scheduler checks it. It has no probe ids: the monitor is this
// probe's by being in its assignment. Each call makes a new one, since a check changes its monitor.
func monitorOf(m probeproto.Monitor) *shared.Monitor {
	mon := &shared.Monitor{
		ID:             m.ID,
		Type:           m.Type,
		Name:           m.Name,
		Interval:       m.Interval,
		Timeout:        m.Timeout,
		MaxRetries:     m.MaxRetries,
		RetryInterval:  m.RetryInterval,
		ResendInterval: m.ResendInterval,
		Active:         true,
		Config:         m.Config,
		UpdatedAt:      m.UpdatedAt,
	}
	if m.Proxy != nil {
		mon.ProxyId = m.Proxy.ID
	}
	return mon
}

func proxyOf(p *probeproto.Proxy) *shared.Proxy {
	if p == nil {
		return nil
	}
	return &shared.Proxy{
		ID:        p.ID,
		Protocol:  p.Protocol,
		Host:      p.Host,
		Port:      p.Port,
		Auth:      p.Auth,
		Username:  p.Username,
		Password:  p.Password,
		UpdatedAt: p.UpdatedAt,
	}
}
