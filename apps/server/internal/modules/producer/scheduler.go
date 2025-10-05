package producer

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// initializeSchedule loads all active monitors and schedules them
func (p *Producer) initializeSchedule() error {
	p.logger.Info("Initializing schedule with active monitors")

	monitors, err := p.monitorService.FindActive(p.ctx)
	if err != nil {
		return fmt.Errorf("failed to find active monitors: %w", err)
	}

	if len(monitors) == 0 {
		p.logger.Info("No active monitors found to schedule")
		return nil
	}

	now := time.Now().UTC()
	pipe := p.rdb.Pipeline()

	for _, mon := range monitors {
		if mon.Interval <= 0 {
			p.logger.Warnw("Skipping monitor with invalid interval", "monitor_id", mon.ID, "interval", mon.Interval)
			continue
		}

		// Store monitor interval for future reference
		p.mu.Lock()
		p.monitorIntervals[mon.ID] = mon.Interval
		p.mu.Unlock()

		// Schedule monitor at next aligned time
		next := nextAligned(now, time.Duration(mon.Interval)*time.Second)
		pipe.ZAdd(p.ctx, SchedDueKey, redis.Z{
			Score:  float64(next.UnixMilli()),
			Member: mon.ID,
		})
	}

	if _, err := pipe.Exec(p.ctx); err != nil {
		return fmt.Errorf("failed to schedule monitors: %w", err)
	}

	p.logger.Infow("Initialized schedule", "monitor_count", len(monitors))
	return nil
}

// runReclaimer periodically reclaims expired leases
func (p *Producer) runReclaimer() {
	defer p.wg.Done()
	ticker := time.NewTicker(ReclaimEvery)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			nowMs := p.redisNowMs()
			result, err := reclaimScript.Run(p.ctx, p.rdb,
				[]string{SchedLeaseKey, SchedDueKey},
				nowMs, 5000).Result()
			if err != nil {
				p.logger.Errorw("Reclaim error", "error", err)
			} else if ids, ok := result.([]interface{}); ok && len(ids) > 0 {
				p.logger.Infow("Reclaimed expired leases", "count", len(ids))
			}
		}
	}
}

// runScheduleRefresher periodically refreshes the schedule with new/updated monitors
func (p *Producer) runScheduleRefresher() {
	defer p.wg.Done()
	ticker := time.NewTicker(p.scheduleRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			if err := p.refreshSchedule(); err != nil {
				p.logger.Errorw("Failed to refresh schedule", "error", err)
			}
		}
	}
}

// refreshSchedule updates the schedule with any new or updated monitors
func (p *Producer) refreshSchedule() error {
	monitors, err := p.monitorService.FindActive(p.ctx)
	if err != nil {
		return fmt.Errorf("failed to find active monitors: %w", err)
	}

	currentMonitorIDs := make(map[string]bool)
	now := time.Now().UTC()
	pipe := p.rdb.Pipeline()

	for _, mon := range monitors {
		if mon.Interval <= 0 {
			continue
		}

		currentMonitorIDs[mon.ID] = true

		p.mu.RLock()
		oldInterval, exists := p.monitorIntervals[mon.ID]
		p.mu.RUnlock()

		// If monitor is new or interval changed, reschedule it
		if !exists || oldInterval != mon.Interval {
			p.mu.Lock()
			p.monitorIntervals[mon.ID] = mon.Interval
			p.mu.Unlock()

			// Remove from both due and lease sets
			pipe.ZRem(p.ctx, SchedDueKey, mon.ID)
			pipe.ZRem(p.ctx, SchedLeaseKey, mon.ID)

			// Schedule at next aligned time
			next := nextAligned(now, time.Duration(mon.Interval)*time.Second)
			pipe.ZAdd(p.ctx, SchedDueKey, redis.Z{
				Score:  float64(next.UnixMilli()),
				Member: mon.ID,
			})

			if !exists {
				p.logger.Infow("Scheduling new monitor", "monitor_id", mon.ID, "interval", mon.Interval)
			} else {
				p.logger.Infow("Rescheduling monitor with updated interval",
					"monitor_id", mon.ID,
					"old_interval", oldInterval,
					"new_interval", mon.Interval)
			}
		}
	}

	// Remove monitors that are no longer active
	p.mu.Lock()
	for monitorID := range p.monitorIntervals {
		if !currentMonitorIDs[monitorID] {
			delete(p.monitorIntervals, monitorID)
			pipe.ZRem(p.ctx, SchedDueKey, monitorID)
			pipe.ZRem(p.ctx, SchedLeaseKey, monitorID)
			p.logger.Infow("Removed inactive monitor from schedule", "monitor_id", monitorID)
		}
	}
	p.mu.Unlock()

	if _, err := pipe.Exec(p.ctx); err != nil {
		return fmt.Errorf("failed to refresh schedule: %w", err)
	}

	return nil
}

// ScheduleMonitor adds or updates a monitor in the schedule
func (p *Producer) ScheduleMonitor(ctx context.Context, monitorID string, intervalSeconds int) error {
	if intervalSeconds <= 0 {
		return fmt.Errorf("invalid interval: %d", intervalSeconds)
	}

	p.mu.Lock()
	p.monitorIntervals[monitorID] = intervalSeconds
	p.mu.Unlock()

	now := time.Now().UTC()
	next := nextAligned(now, time.Duration(intervalSeconds)*time.Second)

	// Remove from lease in case it's there, then add to due
	pipe := p.rdb.Pipeline()
	pipe.ZRem(ctx, SchedLeaseKey, monitorID)
	pipe.ZAdd(ctx, SchedDueKey, redis.Z{
		Score:  float64(next.UnixMilli()),
		Member: monitorID,
	})

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to schedule monitor: %w", err)
	}

	p.logger.Infow("Scheduled monitor", "monitor_id", monitorID, "interval", intervalSeconds, "next_run", next)
	return nil
}

// UnscheduleMonitor removes a monitor from the schedule
func (p *Producer) UnscheduleMonitor(ctx context.Context, monitorID string) error {
	p.mu.Lock()
	delete(p.monitorIntervals, monitorID)
	p.mu.Unlock()

	pipe := p.rdb.Pipeline()
	pipe.ZRem(ctx, SchedDueKey, monitorID)
	pipe.ZRem(ctx, SchedLeaseKey, monitorID)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to unschedule monitor: %w", err)
	}

	p.logger.Infow("Unscheduled monitor", "monitor_id", monitorID)
	return nil
}

// AddMonitor adds a new monitor to the schedule
func (p *Producer) AddMonitor(ctx context.Context, monitorID string) error {
	// Fetch monitor from database
	mon, err := p.monitorService.FindByID(ctx, monitorID)
	if err != nil {
		return fmt.Errorf("failed to find monitor: %w", err)
	}

	if !mon.Active || mon.Interval <= 0 {
		p.logger.Infow("Skipping inactive or invalid monitor", "monitor_id", monitorID, "active", mon.Active, "interval", mon.Interval)
		return nil
	}

	// Schedule the monitor
	return p.ScheduleMonitor(ctx, monitorID, mon.Interval)
}

// UpdateMonitor updates an existing monitor in the schedule
func (p *Producer) UpdateMonitor(ctx context.Context, monitorID string) error {
	// Fetch monitor from database
	mon, err := p.monitorService.FindByID(ctx, monitorID)
	if err != nil {
		return fmt.Errorf("failed to find monitor: %w", err)
	}

	if !mon.Active {
		// If monitor is no longer active, unschedule it
		p.logger.Infow("Monitor became inactive, unscheduling", "monitor_id", monitorID)
		return p.UnscheduleMonitor(ctx, monitorID)
	}

	if mon.Interval <= 0 {
		p.logger.Warnw("Monitor has invalid interval, unscheduling", "monitor_id", monitorID, "interval", mon.Interval)
		return p.UnscheduleMonitor(ctx, monitorID)
	}

	// Reschedule the monitor with updated interval
	return p.ScheduleMonitor(ctx, monitorID, mon.Interval)
}

// RemoveMonitor removes a monitor from the schedule
func (p *Producer) RemoveMonitor(ctx context.Context, monitorID string) error {
	return p.UnscheduleMonitor(ctx, monitorID)
}
