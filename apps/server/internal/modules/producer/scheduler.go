package producer

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// initializeSchedule loads all active monitors and schedules them in a paginated manner
func (p *Producer) initializeSchedule() error {
	p.logger.Info("Initializing schedule with active monitors")

	// Get existing scheduled monitors from Redis (both due and lease sets)
	existingDue, err := p.rdb.ZRangeWithScores(p.ctx, SchedDueKey, 0, -1).Result()
	if err != nil {
		return fmt.Errorf("failed to get existing scheduled monitors from due set: %w", err)
	}

	existingLease, err := p.rdb.ZRangeWithScores(p.ctx, SchedLeaseKey, 0, -1).Result()
	if err != nil {
		return fmt.Errorf("failed to get existing scheduled monitors from lease set: %w", err)
	}

	// Create a map of existing scheduled monitor IDs for quick lookup
	existingMonitorIDs := make(map[string]bool)
	for _, item := range existingDue {
		if monitorID, ok := item.Member.(string); ok {
			existingMonitorIDs[monitorID] = true
		}
	}
	for _, item := range existingLease {
		if monitorID, ok := item.Member.(string); ok {
			existingMonitorIDs[monitorID] = true
		}
	}

	// Pagination settings
	const pageSize = 100
	page := 0
	totalMonitors := 0
	newlyScheduledCount := 0
	removedCount := 0
	activeMonitorIDs := make(map[string]bool)
	now := time.Now().UTC()

	// Process monitors in pages
	for {
		monitors, err := p.monitorService.FindActivePaginated(p.ctx, page, pageSize)
		if err != nil {
			return fmt.Errorf("failed to find active monitors (page %d): %w", page, err)
		}

		if len(monitors) == 0 {
			break
		}

		p.logger.Infow("Processing page of active monitors", "page", page, "count", len(monitors))

		// Track active monitor IDs
		for _, mon := range monitors {
			if mon.Interval > 0 {
				activeMonitorIDs[mon.ID] = true
			}
		}

		// Process monitors in this page
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

			// Only schedule if not already in Redis
			if !existingMonitorIDs[mon.ID] {
				// Schedule monitor at next aligned time
				next := nextAligned(now, time.Duration(mon.Interval)*time.Second)
				pipe.ZAdd(p.ctx, SchedDueKey, redis.Z{
					Score:  float64(next.UnixMilli()),
					Member: mon.ID,
				})
				newlyScheduledCount++
				p.logger.Debugw("Scheduled new monitor", "monitor_id", mon.ID, "next_run", next)
			} else {
				p.logger.Debugw("Monitor already scheduled, skipping", "monitor_id", mon.ID)
			}
		}

		if _, err := pipe.Exec(p.ctx); err != nil {
			return fmt.Errorf("failed to schedule monitors (page %d): %w", page, err)
		}

		totalMonitors += len(monitors)
		page++

		// If we got fewer monitors than the page size, we've reached the end
		if len(monitors) < pageSize {
			break
		}
	}

	// Remove monitors that are in Redis but not active in database
	pipe := p.rdb.Pipeline()
	for monitorID := range existingMonitorIDs {
		if !activeMonitorIDs[monitorID] {
			pipe.ZRem(p.ctx, SchedDueKey, monitorID)
			pipe.ZRem(p.ctx, SchedLeaseKey, monitorID)
			p.mu.Lock()
			delete(p.monitorIntervals, monitorID)
			p.mu.Unlock()
			removedCount++
			p.logger.Infow("Removing stale monitor from schedule", "monitor_id", monitorID)
		}
	}

	if _, err := pipe.Exec(p.ctx); err != nil {
		return fmt.Errorf("failed to remove stale monitors: %w", err)
	}

	p.logger.Infow("Initialized schedule",
		"total_active_monitors", totalMonitors,
		"newly_scheduled", newlyScheduledCount,
		"already_scheduled", len(existingMonitorIDs)-removedCount,
		"removed_stale", removedCount)
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

// refreshSchedule updates the schedule with any new or updated monitors in a paginated manner
func (p *Producer) refreshSchedule() error {
	// Pagination settings
	const pageSize = 100
	page := 0
	currentMonitorIDs := make(map[string]bool)
	now := time.Now().UTC()

	// Process monitors in pages
	for {
		monitors, err := p.monitorService.FindActivePaginated(p.ctx, page, pageSize)
		if err != nil {
			return fmt.Errorf("failed to find active monitors (page %d): %w", page, err)
		}

		if len(monitors) == 0 {
			break
		}

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

		if _, err := pipe.Exec(p.ctx); err != nil {
			return fmt.Errorf("failed to refresh schedule (page %d): %w", page, err)
		}

		page++

		// If we got fewer monitors than the page size, we've reached the end
		if len(monitors) < pageSize {
			break
		}
	}

	// Remove monitors that are no longer active
	pipe := p.rdb.Pipeline()
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
		return fmt.Errorf("failed to remove inactive monitors: %w", err)
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
