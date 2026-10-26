package probe

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/events"

	"go.uber.org/zap"
)

const (
	// DefaultOfflineAfter is how long a probe's instances may go unseen before the probe counts as
	// offline: PROBE_OFFLINE_AFTER's default
	DefaultOfflineAfter = 3 * time.Minute
	// MinOfflineAfter is the shortest PROBE_OFFLINE_AFTER, which the binaries that run the sweep
	// refuse to start under. It is the config's floor, here for the checks that pin what it leaves
	// a restarting probe: see RetiredAt.
	MinOfflineAfter = config.MinProbeOfflineAfter
	// DefaultSweepInterval is how often the sweep looks at the probes
	DefaultSweepInterval = 30 * time.Second
	// DefaultInstanceRetention is how long an instance may go unseen before the sweep forgets it.
	// It is the config's cap on PROBE_OFFLINE_AFTER, so that the sweep never finds a probe online
	// on an instance that it is about to prune.
	DefaultInstanceRetention = config.MaxProbeOfflineAfter
)

// SweepConfig is what a Sweep reads and calls
type SweepConfig struct {
	// Repo is where the probes and their instances are
	Repo     Repository
	EventBus events.EventBus
	// OfflineAfter is how long an online probe's instances may all go unseen before the probe is
	// set offline. DefaultOfflineAfter when it's zero.
	OfflineAfter time.Duration
	// Interval is how often the sweep runs. DefaultSweepInterval when it's zero.
	Interval time.Duration
	// InstanceRetention is how long an instance may go unseen before it is removed.
	// DefaultInstanceRetention when it's zero.
	InstanceRetention time.Duration
	// InstanceOnlineWindow is how long after its last request an instance counts as connected:
	// the window the protocol deals a probe's monitors within. InstanceOnlineWindow when it's zero.
	InstanceOnlineWindow time.Duration
	// StartedAt is when the process started. The sweep sets no probe offline until OfflineAfter
	// has passed since: after an outage every instance looks quiet, and the probes need that long
	// to reconnect. The moment NewSweep is called when it's zero.
	StartedAt time.Time
	// Now is the clock the sweep measures by. The local clock is used when it's nil.
	Now func() time.Time
	// Silent, when set, has the sweep report the local probe offline when it has monitors to run and
	// no instance. Only the cluster on SQLite and PostgreSQL sets it.
	Silent *SilentWatch
	Logger *zap.SugaredLogger
}

// Sweep keeps the probes' status honest. A request from a probe sets it online the moment it
// arrives, but nothing happens when a probe goes quiet, so the sweep looks for that. Every
// interval it
//   - sets an online probe offline when none of its instances was seen within OfflineAfter, and
//     publishes ProbeOffline;
//   - sets an offline probe online when one of its instances was, as a backstop for a request
//     that couldn't do it, and publishes ProbeOnline;
//   - publishes InstancesChanged when the instances a probe is dealt among are no longer the ones
//     it saw last time, so that the probe's waiting assignment polls deal again;
//   - removes the instances that went unseen for InstanceRetention;
//   - on a cluster on SQLite or PostgreSQL (SweepConfig.Silent), publishes ProbeOffline for the local
//     probe once when it has monitors to run and no instance has said hello within OfflineAfter of
//     this replica taking the leader lock: the probe was never online, so it never goes quiet.
//
// It sets no probe offline until OfflineAfter has passed since the process started, so that after
// an outage the probes have time to reconnect before they are announced offline. The rest of the
// pass goes on meanwhile.
//
// Only one sweep should run at a time: cluster mode runs it on the api replica that holds the
// leader lock.
type Sweep struct {
	repo         Repository
	eventBus     events.EventBus
	offlineAfter time.Duration
	interval     time.Duration
	retention    time.Duration
	onlineWindow time.Duration
	started      time.Time
	now          func() time.Time
	silent       *SilentWatch
	logger       *zap.SugaredLogger

	mu sync.Mutex // one pass at a time; guards online and termStart
	// online is the ids of each probe's online instances as of the last pass
	online map[string][]string
	// termStart is when this replica began its term as the leader: the instances of a probe that
	// has none get OfflineAfter from then to say hello. Zero before the first term.
	termStart time.Time
}

// ProvideSweep provides the sweep, offline after PROBE_OFFLINE_AFTER. The binary that runs it
// provides the event bus: single mode's, or the cluster api's.
func ProvideSweep(repo Repository, eventBus events.EventBus, cfg *config.Config, logger *zap.SugaredLogger) *Sweep {
	return NewSweep(SweepConfig{
		Repo:         repo,
		EventBus:     eventBus,
		OfflineAfter: cfg.ProbeOfflineAfter,
		Logger:       logger,
	})
}

// ProvideSilentSweep provides the sweep of the cluster api on SQLite and PostgreSQL, which also
// reports the local probe offline when it has monitors to run and no instance (see SilentWatch)
func ProvideSilentSweep(repo Repository, eventBus events.EventBus, cfg *config.Config, silent *SilentWatch, logger *zap.SugaredLogger) *Sweep {
	return NewSweep(SweepConfig{
		Repo:         repo,
		EventBus:     eventBus,
		OfflineAfter: cfg.ProbeOfflineAfter,
		Silent:       silent,
		Logger:       logger,
	})
}

// NewSweep creates a sweep
func NewSweep(cfg SweepConfig) *Sweep {
	s := &Sweep{
		repo:         cfg.Repo,
		eventBus:     cfg.EventBus,
		offlineAfter: cfg.OfflineAfter,
		interval:     cfg.Interval,
		retention:    cfg.InstanceRetention,
		onlineWindow: cfg.InstanceOnlineWindow,
		started:      cfg.StartedAt,
		now:          cfg.Now,
		silent:       cfg.Silent,
		logger:       cfg.Logger,
		online:       make(map[string][]string),
	}
	if s.offlineAfter <= 0 {
		s.offlineAfter = DefaultOfflineAfter
	}
	if s.interval <= 0 {
		s.interval = DefaultSweepInterval
	}
	if s.retention <= 0 {
		s.retention = DefaultInstanceRetention
	}
	if s.onlineWindow <= 0 {
		s.onlineWindow = InstanceOnlineWindow
	}
	if s.now == nil {
		s.now = func() time.Time { return time.Now().UTC() }
	}
	if s.started.IsZero() {
		s.started = s.now()
	}
	if s.logger == nil {
		s.logger = zap.NewNop().Sugar()
	}
	s.logger = s.logger.Named("[probe-sweep]")
	return s
}

// Run sweeps every interval until ctx is done
func (s *Sweep) Run(ctx context.Context) {
	// The last pass of an earlier run says nothing about the instances now
	s.BeginTerm()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Probe sweep stopped")
			return
		case <-ticker.C:
			err := s.sweepOnce(ctx)
			if err != nil && ctx.Err() == nil {
				s.logger.Errorw("Probe sweep failed", "error", err)
			}
		}
	}
}

// BeginTerm starts a term of the sweep as the leader: what the last term learned about the instances
// is forgotten, and the instances of a probe that has none get OfflineAfter from now. Run does it.
func (s *Sweep) BeginTerm() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.online = make(map[string][]string)
	s.termStart = s.now().UTC()
}

// sweepOnce is one pass over every probe
func (s *Sweep) sweepOnce(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now().UTC()
	probes, err := s.repo.FindAll(ctx)
	if err != nil {
		return err
	}

	present := make(map[string]struct{}, len(probes))
	for _, p := range probes {
		present[p.ID] = struct{}{}
		instances, err := s.repo.FindInstances(ctx, p.ID)
		if err != nil {
			s.logger.Errorw("Failed to find the instances of a probe", "probeId", p.ID, "error", err)
			continue
		}
		s.checkSilent(ctx, p, instances, now)
		s.checkStatus(ctx, p, instances, now)
		s.checkInstances(ctx, p, instances, now)
	}
	// A deleted probe has no instances to compare any more
	for id := range s.online {
		if _, ok := present[id]; !ok {
			delete(s.online, id)
		}
	}

	removed, err := s.repo.DeleteInstancesUnseenSince(ctx, now.Add(-s.retention))
	if err != nil {
		return fmt.Errorf("failed to remove the instances unseen for %s: %w", s.retention, err)
	}
	if removed > 0 {
		s.logger.Infow("Removed probe instances that went unseen", "removed", removed, "unseenFor", s.retention.String())
	}
	return nil
}

// checkStatus sets the probe offline or online by when its instances were last seen
func (s *Sweep) checkStatus(ctx context.Context, p *Model, instances []*Instance, now time.Time) {
	var lastSeen time.Time
	for _, instance := range instances {
		if instance.LastSeenAt.After(lastSeen) {
			lastSeen = instance.LastSeenAt
		}
	}
	quiet := len(instances) == 0 || now.Sub(lastSeen) > s.offlineAfter
	// A probe that is quiet since before the process started may only have had no server to reach
	grace := now.Sub(s.started) < s.offlineAfter

	switch {
	case p.Status == StatusOnline && quiet && !grace:
		since := lastSeen
		if len(instances) == 0 {
			// There is no instance to have seen it last: they were pruned, as after a server outage
			// longer than InstanceRetention. (An instance that says bye is retired, not removed, so
			// it counts from its connection's end.)
			since = now
		} else {
			since = s.lastContact(ctx, p, lastSeen)
		}
		// The probe went offline when it was last seen, which the history records, and not when
		// the sweep found out
		changed, err := s.repo.UpdateStatus(ctx, p.ID, StatusOnline, StatusOffline, since.UTC())
		if err != nil {
			s.logger.Errorw("Failed to set a probe offline", "probeId", p.ID, "error", err)
			return
		}
		if !changed {
			// Another sweep, or a request, got there first
			return
		}
		s.logger.Infow("Probe is offline", "probeId", p.ID, "name", p.Name, "since", since, "disabled", p.Disabled)
		s.markAnnounced(ctx, p)
		if p.Disabled {
			// An admin disabled it: its token is refused, so going quiet is expected, and no one is told
			return
		}
		publish(ctx, s, ProbeOffline, Event{ProbeID: p.ID, ProbeName: p.Name, Since: since.UTC()})
	// A disabled probe's token is refused, so no request can have brought it back
	case p.Status == StatusOffline && !quiet && !p.Disabled:
		changed, err := s.repo.UpdateStatus(ctx, p.ID, StatusOffline, StatusOnline, lastSeen.UTC())
		if err != nil {
			s.logger.Errorw("Failed to set a probe online", "probeId", p.ID, "error", err)
			return
		}
		if !changed {
			return
		}
		s.logger.Infow("Probe is back online", "probeId", p.ID, "name", p.Name)
		publish(ctx, s, ProbeOnline, Event{ProbeID: p.ID, ProbeName: p.Name, Since: lastSeen.UTC()})
	}
}

// lastContact is when the probe's instances were last heard from. An instance's last seen time
// is a minute early when it said bye (see RetiredAt), and its connection keeps the time of the bye
// itself, so the later of the two is when the probe went offline. A probe whose connections can't
// be read counts from its instances' last seen time.
func (s *Sweep) lastContact(ctx context.Context, p *Model, lastSeen time.Time) time.Time {
	connections, err := s.repo.FindInstanceConnections(ctx, p.ID, lastSeen)
	if err != nil {
		s.logger.Warnw("Failed to read the connections of a probe, it went offline when its instances were last seen", "probeId", p.ID, "error", err)
		return lastSeen
	}
	contact := lastSeen
	for _, c := range connections {
		if c.LastSeenAt.After(contact) {
			contact = c.LastSeenAt
		}
	}
	return contact
}

// checkInstances tells the waiting polls of the probe when the instances it is dealt among are
// not the ones it saw on the last pass. The first pass only learns them: hello and bye announce
// their own changes, and a poll that was waiting when this process started deals again on its
// next request anyway.
func (s *Sweep) checkInstances(ctx context.Context, p *Model, instances []*Instance, now time.Time) {
	online := OnlineInstances(instances, now, s.onlineWindow)
	previous, known := s.online[p.ID]
	s.online[p.ID] = online
	if !known || slices.Equal(previous, online) {
		return
	}
	s.logger.Debugw("The online instances of a probe changed", "probeId", p.ID, "before", previous, "after", online)
	publish(ctx, s, InstancesChanged, InstancesChangedEvent{ProbeID: p.ID})
}

// publish tells the other processes. The change is stored already, so a failure to say so is
// logged and not retried.
func publish[T any](ctx context.Context, s *Sweep, topic events.Topic[T], payload T) {
	if err := events.Publish(ctx, s.eventBus, topic, payload); err != nil {
		s.logger.Errorw("Failed to publish a probe event", "topic", topic.Name(), "error", err)
	}
}
