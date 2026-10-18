package producer

import (
	"context"
	"fmt"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/worker"
)

// WatchdogInterval is how often the leader looks for monitors that stopped being checked
const WatchdogInterval = time.Minute

// runWatchdog looks for stale monitors every watchdogInterval until ctx is done (leader only)
func (p *Producer) runWatchdog(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(p.watchdogInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Stale-monitor watchdog stopped")
			return
		case <-ticker.C:
			if err := p.checkStaleMonitors(ctx); err != nil {
				p.logger.Errorw("Stale-monitor watchdog run failed", "error", err)
			}
		}
	}
}

// checkStaleMonitors finds active monitors whose latest heartbeat is older than their
// stale threshold. A newly stale monitor gets a warning and a check right away. If it's
// still stale once that check has had time to finish, its channels are notified once.
// A notified monitor that's checked again gets a recovery notification.
func (p *Producer) checkStaleMonitors(ctx context.Context) error {
	now := time.UnixMilli(p.redisNowMs()).UTC()

	known, err := p.staleStore.All(ctx)
	if err != nil {
		return err
	}

	const pageSize = 100
	seen := make(map[string]bool)
	for page := 0; ; page++ {
		monitors, err := p.monitorService.FindActivePaginated(ctx, page, pageSize)
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
			if err := p.checkStaleMonitor(ctx, mon, now, entry, wasStale); err != nil {
				p.logger.Errorw("Failed to check whether monitor is stale", "monitor_id", mon.ID, "error", err)
			}
		}
	}

	// Paused and deleted monitors aren't stale
	var gone []string
	for id := range known {
		if !seen[id] {
			gone = append(gone, id)
		}
	}
	if err := p.staleStore.Delete(ctx, gone...); err != nil {
		return err
	}

	return p.staleStore.SetRanAt(ctx, now)
}

func (p *Producer) checkStaleMonitor(ctx context.Context, mon *monitor.Model, now time.Time, entry stale.Entry, wasStale bool) error {
	since, err := p.lastCheckedAt(ctx, mon)
	if err != nil {
		return err
	}

	if now.Sub(since) <= stale.Threshold(mon.Interval, mon.Timeout) {
		if !wasStale {
			return nil
		}
		p.logger.Infow("Monitor is being checked again", "monitor_id", mon.ID, "monitor_name", mon.Name)
		if err := p.staleStore.Delete(ctx, mon.ID); err != nil {
			return err
		}
		if entry.Notified {
			p.publishStale(events.MonitorStaleRecovered, mon, entry.Since)
		}
		return nil
	}

	if !wasStale {
		p.logger.Warnw("Monitor has not been checked in too long, enqueueing a check",
			"monitor_id", mon.ID,
			"monitor_name", mon.Name,
			"last_checked_at", since)
		if err := p.scheduleMonitor(ctx, mon.ID, mon.Interval, true); err != nil {
			return err
		}
		return p.staleStore.Put(ctx, stale.Entry{MonitorID: mon.ID, Since: since, DetectedAt: now})
	}

	// Give the re-enqueued check the time it's allowed before notifying
	if entry.Notified || now.Sub(entry.DetectedAt) < worker.HealthCheckTaskTimeout(mon.Timeout) {
		return nil
	}
	p.logger.Warnw("Monitor is still not checked after a re-enqueue, notifying",
		"monitor_id", mon.ID,
		"monitor_name", mon.Name,
		"last_checked_at", entry.Since)
	entry.Notified = true
	if err := p.staleStore.Put(ctx, entry); err != nil {
		return err
	}
	p.publishStale(events.MonitorStale, mon, entry.Since)
	return nil
}

// lastCheckedAt is the time of the monitor's latest heartbeat, or of its last save or
// resume if that's later, so a monitor that was just resumed isn't stale
func (p *Producer) lastCheckedAt(ctx context.Context, mon *monitor.Model) (time.Time, error) {
	since := mon.UpdatedAt
	latest, err := p.heartbeatService.FindByMonitorIDPaginated(ctx, mon.ID, 1, 0, nil, false)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to find latest heartbeat: %w", err)
	}
	if len(latest) > 0 && latest[0].Time.After(since) {
		since = latest[0].Time
	}
	return since.UTC(), nil
}

func (p *Producer) publishStale(eventType events.EventType, mon *monitor.Model, since time.Time) {
	p.eventBus.Publish(events.Event{
		Type:    eventType,
		Payload: &stale.Event{MonitorID: mon.ID, MonitorName: mon.Name, Since: since},
	})
}
