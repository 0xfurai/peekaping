package stale

import (
	"context"
	"fmt"
	"slices"
	"time"

	"go.uber.org/zap"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/shared"
)

// WatchdogInterval is how often the watchdog looks for monitors that stopped being checked
const WatchdogInterval = time.Minute

// Rechecker gets a monitor checked right away, without waiting for its next scheduled check. It
// checks the monitors of the local probe only, and refuses one that a remote probe runs.
type Rechecker interface {
	RunNow(ctx context.Context, monitorID string) error
}

// MonitorFinder is the part of the monitor service the watchdog reads
type MonitorFinder interface {
	FindByID(ctx context.Context, id string) (*shared.Monitor, error)
	FindActivePaginated(ctx context.Context, page int, limit int) ([]*shared.Monitor, error)
}

// HeartbeatFinder is the part of the heartbeat service the watchdog reads
type HeartbeatFinder interface {
	FindByMonitorIDPaginated(ctx context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*shared.HeartBeatModel, error)
}

// ProbeOfflineFunc reports whether the probe a monitor is assigned to is offline. A monitor
// whose probe is offline isn't being checked for a reason that probe's own notification gives, so
// the watchdog leaves it alone. The local probe never counts as offline. The monitor is the one
// the watchdog holds, with its probes.
type ProbeOfflineFunc func(ctx context.Context, mon *shared.Monitor) (bool, error)

// ProbeInfo is what the watchdog needs to know of a probe
type ProbeInfo struct {
	Name string
	// SupportedTypes are the monitor types the probe runs. Empty means every type.
	SupportedTypes []string
}

// ProbeLookup tells the watchdog about the probes a monitor runs on. The probe module's service
// stands behind it, wired in internal/app, so that this package doesn't import the probe module.
type ProbeLookup interface {
	// FindProbe returns nil when there is no such probe
	FindProbe(ctx context.Context, id string) (*ProbeInfo, error)
}

// WatchdogConfig is what a Watchdog reads and calls
type WatchdogConfig struct {
	Store Store
	// Rechecker gets the monitors of the local probe checked again. Nil in cluster mode, where
	// no process of the watchdog's own runs checks: every stale monitor is then recorded and
	// notified as one on a remote probe is.
	Rechecker  Rechecker
	Monitors   MonitorFinder
	Heartbeats HeartbeatFinder
	EventBus   events.EventBus
	// CheckTimeout is the longest a check of a monitor with the given timeout (in seconds)
	// can take to finish and be ingested. A newly stale monitor is rechecked, and notified
	// once that time has passed without a heartbeat.
	CheckTimeout func(timeoutSeconds int) time.Duration
	// Now is the clock the watchdog measures staleness by. Cluster mode's api replicas share
	// Redis's clock. The local clock is used when it's nil.
	Now func() time.Time
	// Interval is how often the watchdog runs; WatchdogInterval when it's zero
	Interval time.Duration
	// ProbeOffline says whether a monitor's probe is offline. The watchdog skips such a monitor
	// entirely: no recheck, no record, no notification. Nil means no monitor's probe is ever
	// offline.
	ProbeOffline ProbeOfflineFunc
	// Probes says which of a monitor's probes run its type, so that the notification of a stale
	// monitor can say that none checks it for that reason. Nil means every probe runs every type.
	Probes ProbeLookup
	Logger *zap.SugaredLogger
}

// Watchdog looks for active monitors that stopped being checked, gets them checked
// again, and notifies their channels if that doesn't help. It records the stale
// monitors in its Store.
type Watchdog struct {
	store        Store
	rechecker    Rechecker
	monitors     MonitorFinder
	heartbeats   HeartbeatFinder
	eventBus     events.EventBus
	checkTimeout func(timeoutSeconds int) time.Duration
	now          func() time.Time
	interval     time.Duration
	probeOffline ProbeOfflineFunc
	probes       ProbeLookup
	logger       *zap.SugaredLogger
}

// NewWatchdog creates a watchdog
func NewWatchdog(cfg WatchdogConfig) *Watchdog {
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = WatchdogInterval
	}
	return &Watchdog{
		store:        cfg.Store,
		rechecker:    cfg.Rechecker,
		monitors:     cfg.Monitors,
		heartbeats:   cfg.Heartbeats,
		eventBus:     cfg.EventBus,
		checkTimeout: cfg.CheckTimeout,
		now:          now,
		interval:     interval,
		probeOffline: cfg.ProbeOffline,
		probes:       cfg.Probes,
		logger:       cfg.Logger,
	}
}

// Run looks for stale monitors every interval until ctx is done. Only one watchdog should
// run at a time: cluster mode runs it on the api replica that holds the leader lock.
func (w *Watchdog) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Stale-monitor watchdog stopped")
			return
		case <-ticker.C:
			// A run that fails because ctx is done is the watchdog stopping, not a failure
			if err := w.checkStaleMonitors(ctx); err != nil && ctx.Err() == nil {
				w.logger.Errorw("Stale-monitor watchdog run failed", "error", err)
			}
		}
	}
}

// checkStaleMonitors finds active monitors whose latest heartbeat is older than their
// stale threshold, whichever probe runs them. A newly stale monitor gets a warning and a check
// right away, unless a remote probe runs it: it gets no check from here. If it's still stale
// once that check has had time to finish, its channels are notified once.
// A notified monitor that's checked again gets a recovery notification.
func (w *Watchdog) checkStaleMonitors(ctx context.Context) error {
	now := w.now().UTC()

	known, err := w.store.All(ctx)
	if err != nil {
		return err
	}

	const pageSize = 100
	seen := make(map[string]bool)
	for page := 0; ; page++ {
		monitors, err := w.monitors.FindActivePaginated(ctx, page, pageSize)
		if err != nil {
			return fmt.Errorf("failed to find active monitors (page %d): %w", page, err)
		}
		if len(monitors) == 0 {
			break
		}
		for _, mon := range monitors {
			if !mon.Active || mon.Interval <= 0 {
				continue
			}
			seen[mon.ID] = true
			entry, wasStale := known[mon.ID]
			if err := w.checkStaleMonitor(ctx, mon, now, entry, wasStale); err != nil {
				if ctx.Err() != nil {
					return err // the rest would fail the same way
				}
				w.logger.Errorw("Failed to check whether monitor is stale", "monitor_id", mon.ID, "error", err)
			}
		}
	}

	// Paused and deleted monitors aren't stale. A monitor paused during the scan shifts
	// the pages, so check each unseen one before forgetting it.
	var gone []string
	for id := range known {
		if seen[id] {
			continue
		}
		mon, err := w.monitors.FindByID(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			w.logger.Errorw("Failed to find stale monitor", "monitor_id", id, "error", err)
			continue
		}
		if mon == nil || !mon.Active {
			gone = append(gone, id)
		}
	}
	if err := w.store.Delete(ctx, gone...); err != nil {
		return err
	}

	return w.store.SetRanAt(ctx, now)
}

func (w *Watchdog) checkStaleMonitor(ctx context.Context, mon *shared.Monitor, now time.Time, entry Entry, wasStale bool) error {
	// A monitor whose probe is offline has no checks to be late: the probe's own notification
	// says so. A record of a notification stays, so the monitor's recovery is still reported
	// once it is checked again. A record of a recheck that wasn't notified yet is forgotten:
	// that recheck can't run while the probe is offline, and the time it was given would run out
	// by the time the probe is back, which would notify with no recheck made. The monitor is
	// found stale afresh then, with a new recheck and a new time to be checked in.
	if w.probeOffline != nil {
		offline, err := w.probeOffline(ctx, mon)
		if err != nil {
			return fmt.Errorf("failed to find out whether the monitor's probe is offline: %w", err)
		}
		if offline {
			w.logger.Debugw("Skipping a monitor whose probe is offline", "monitor_id", mon.ID, "monitor_name", mon.Name)
			if wasStale && !entry.Notified {
				return w.store.Delete(ctx, mon.ID)
			}
			return nil
		}
	}

	heartbeatAt, err := w.latestHeartbeatAt(ctx, mon.ID)
	if err != nil {
		return err
	}
	// A monitor that was just saved or resumed isn't stale yet
	since := mon.UpdatedAt.UTC()
	if heartbeatAt.After(since) {
		since = heartbeatAt
	}

	if now.Sub(since) <= Threshold(mon.Interval, mon.Timeout) {
		if !wasStale {
			return nil
		}
		if !heartbeatAt.After(entry.Since) {
			// Saved or resumed, but still not checked. After a notification it's the
			// same stale period; before one, the save starts the wait over.
			if entry.Notified {
				return nil
			}
			return w.store.Delete(ctx, mon.ID)
		}
		w.logger.Infow("Monitor is being checked again", "monitor_id", mon.ID, "monitor_name", mon.Name)
		if err := w.store.Delete(ctx, mon.ID); err != nil {
			return err
		}
		if entry.Notified {
			w.publishStale(ctx, MonitorStaleRecovered, Event{MonitorID: mon.ID, MonitorName: mon.Name, Since: entry.Since})
		}
		return nil
	}

	if !wasStale {
		// The rechecker checks the monitors of the local probe only, and cluster mode has none: no
		// process of its own runs checks there. A monitor with no recheck to make gets none from
		// here, but the time a recheck is given still runs, so that its channels are notified if
		// it stays unchecked.
		if w.rechecker == nil || !mon.RunsOn(shared.LocalProbeID) {
			w.logger.Warnw("Monitor on a probe has not been checked in too long",
				"monitor_id", mon.ID,
				"monitor_name", mon.Name,
				"last_checked_at", since)
			return w.store.Put(ctx, Entry{MonitorID: mon.ID, Since: since, DetectedAt: now})
		}
		w.logger.Warnw("Monitor has not been checked in too long, checking it again",
			"monitor_id", mon.ID,
			"monitor_name", mon.Name,
			"last_checked_at", since)
		if err := w.rechecker.RunNow(ctx, mon.ID); err != nil {
			return err
		}
		return w.store.Put(ctx, Entry{MonitorID: mon.ID, Since: since, DetectedAt: now})
	}

	// Give that check the time it's allowed before notifying
	if entry.Notified || now.Sub(entry.DetectedAt) < w.checkTimeout(mon.Timeout) {
		return nil
	}
	w.logger.Warnw("Monitor is still not checked after checking it again, notifying",
		"monitor_id", mon.ID,
		"monitor_name", mon.Name,
		"last_checked_at", entry.Since)
	entry.Notified = true
	if err := w.store.Put(ctx, entry); err != nil {
		return err
	}
	// Only this notification says why: the one that the monitor is checked again has nothing to add
	w.publishStale(ctx, MonitorStale, Event{
		MonitorID:         mon.ID,
		MonitorName:       mon.Name,
		Since:             entry.Since,
		UnsupportedProbes: w.unsupportedProbes(ctx, mon),
	})
	return nil
}

// latestHeartbeatAt is the time of the monitor's latest heartbeat, zero if it has none
func (w *Watchdog) latestHeartbeatAt(ctx context.Context, monitorID string) (time.Time, error) {
	latest, err := w.heartbeats.FindByMonitorIDPaginated(ctx, monitorID, 1, 0, nil, false)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to find latest heartbeat: %w", err)
	}
	if len(latest) == 0 {
		return time.Time{}, nil
	}
	return latest[0].Time.UTC(), nil
}

func (w *Watchdog) publishStale(ctx context.Context, topic events.Topic[Event], event Event) {
	if err := events.Publish(ctx, w.eventBus, topic, event); err != nil {
		w.logger.Errorw("Failed to publish stale monitor event", "topic", topic.Name(), "monitor_id", event.MonitorID, "error", err)
	}
}

// unsupportedProbes names the probes of the monitor that don't run its type, in the monitor's
// order. A probe with no supported types runs every type, as does the local probe, which isn't
// read. A probe that doesn't exist, or can't be read, isn't named: the notification goes out
// without that reason rather than not at all.
func (w *Watchdog) unsupportedProbes(ctx context.Context, mon *shared.Monitor) []string {
	if w.probes == nil {
		return nil
	}
	var names []string
	for _, probeID := range mon.ProbeIDs {
		if probeID == shared.LocalProbeID {
			continue
		}
		found, err := w.probes.FindProbe(ctx, probeID)
		if err != nil {
			w.logger.Warnw("Failed to find a probe of a stale monitor", "monitor_id", mon.ID, "probe_id", probeID, "error", err)
			continue
		}
		if found != nil && len(found.SupportedTypes) > 0 && !slices.Contains(found.SupportedTypes, mon.Type) {
			names = append(names, found.Name)
		}
	}
	return names
}
