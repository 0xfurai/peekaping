package producer

import (
	"fmt"
	"time"

	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/worker"
)

// runProducer is the main producer loop
func (p *Producer) runProducer() error {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		default:
		}

		nowMs := p.redisNowMs()

		// Atomically claim a batch of due items
		idsAny, err := claimScript.Run(p.ctx, p.rdb,
			[]string{SchedDueKey, SchedLeaseKey},
			nowMs, BatchClaim, int64(LeaseTTL/time.Millisecond),
		).Result()
		if err != nil {
			p.logger.Errorw("Claim error", "error", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		ids := toStringSlice(idsAny)
		if len(ids) == 0 {
			// Sleep until next check
			time.Sleep(ClaimTick)
			continue
		}

		p.logger.Debugw("Claimed monitors for scheduling", "count", len(ids))

		// Process each claimed monitor
		pipe := p.rdb.Pipeline()
		for _, monitorID := range ids {
			interval, err := p.processMonitor(monitorID, nowMs)
			if err != nil {
				p.logger.Errorw("Failed to process monitor",
					"monitor_id", monitorID,
					"error", err)
				// Don't reschedule on error - let lease expire and be reclaimed
				continue
			}

			// Skip rescheduling if interval is invalid (e.g., monitor was deleted or deactivated)
			if interval <= 0 {
				p.logger.Debugw("Skipping reschedule for monitor with invalid interval", "monitor_id", monitorID)
				continue
			}

			// Calculate next execution time
			next := nextAligned(time.UnixMilli(nowMs).UTC(), time.Duration(interval)*time.Second)
			pipe.Eval(p.ctx, reschedLua, []string{SchedLeaseKey, SchedDueKey}, monitorID, next.UnixMilli())
		}

		if _, err := pipe.Exec(p.ctx); err != nil {
			p.logger.Errorw("Resched pipeline error", "error", err)
		}
	}
}

// processMonitor loads monitor config and enqueues a health check task
// Returns the monitor interval (for rescheduling) and any error
func (p *Producer) processMonitor(monitorID string, nowMs int64) (int, error) {
	start := time.Now()
	// Fetch monitor from database
	mon, err := p.monitorService.FindByID(p.ctx, monitorID)
	if err != nil {
		return 0, fmt.Errorf("failed to find monitor: %w", err)
	}

	// Check if monitor exists (it might have been deleted)
	if mon == nil {
		p.logger.Warnw("Monitor not found, skipping", "monitor_id", monitorID)
		return 0, nil
	}

	if !mon.Active {
		p.logger.Infow("Skipping inactive monitor", "monitor_id", monitorID)
		return 0, nil
	}

	// Check if monitor is under maintenance
	isUnderMaintenance := false
	maintenances, err := p.maintenanceService.GetMaintenancesByMonitorID(p.ctx, monitorID)
	if err != nil {
		p.logger.Errorw("Failed to get maintenances", "monitor_id", monitorID, "error", err)
	} else {
		for _, maint := range maintenances {
			underMaintenance, err := p.maintenanceService.IsUnderMaintenance(p.ctx, maint)
			if err != nil {
				p.logger.Warnw("Failed to check maintenance status",
					"monitor_id", monitorID,
					"maintenance_id", maint.ID,
					"error", err)
				continue
			}
			if underMaintenance {
				isUnderMaintenance = true
				break
			}
		}
	}

	// Fetch proxy if configured
	var proxyData *worker.ProxyData
	if mon.ProxyId != "" {
		proxyModel, err := p.proxyService.FindByID(p.ctx, mon.ProxyId)
		if err != nil {
			p.logger.Warnw("Failed to fetch proxy, continuing without it",
				"monitor_id", monitorID,
				"proxy_id", mon.ProxyId,
				"error", err)
		} else {
			proxyData = &worker.ProxyData{
				ID:       proxyModel.ID,
				Protocol: proxyModel.Protocol,
				Host:     proxyModel.Host,
				Port:     proxyModel.Port,
				Auth:     proxyModel.Auth,
				Username: proxyModel.Username,
				Password: proxyModel.Password,
			}
		}
	}

	// TODO: fix this
	// Check if we should check certificate expiry
	// This is typically enabled by default for http and tcp monitors
	checkCertExpiry := false
	if mon.Type == "http" || mon.Type == "tcp" {
		// Certificate checking is typically always enabled for these monitor types
		// The certificate service handles notification settings
		checkCertExpiry = true
	}

	// Create health check task payload
	payload := worker.HealthCheckTaskPayload{
		MonitorID:          mon.ID,
		MonitorName:        mon.Name,
		MonitorType:        mon.Type,
		Interval:           mon.Interval,
		Timeout:            mon.Timeout,
		MaxRetries:         mon.MaxRetries,
		RetryInterval:      mon.RetryInterval,
		ResendInterval:     mon.ResendInterval,
		Config:             mon.Config,
		Proxy:              proxyData,
		ScheduledAt:        time.UnixMilli(nowMs).UTC(),
		IsUnderMaintenance: isUnderMaintenance,
		CheckCertExpiry:    checkCertExpiry,
	}

	// Enqueue task to worker queue
	opts := &queue.EnqueueOptions{
		Queue:     "healthcheck",
		MaxRetry:  0,
		Timeout:   time.Duration(mon.Timeout) * time.Second,
		Retention: 0 * time.Minute,
	}

	// Use EnqueueUnique to prevent duplicate tasks from being scheduled
	// The unique key is based on monitor ID, and TTL is 2x the interval to ensure
	// no duplicate tasks are created even if there are scheduling delays
	uniqueKey := fmt.Sprintf("healthcheck:%s", mon.ID)
	ttl := time.Duration(mon.Interval*2) * time.Second

	_, err = p.queueService.EnqueueUnique(p.ctx, worker.TaskTypeHealthCheck, payload, uniqueKey, ttl, opts)
	if err != nil {
		return 0, fmt.Errorf("failed to enqueue health check: %w", err)
	}

	p.logger.Debugw("Enqueued health check",
		"monitor_id", mon.ID,
		"monitor_name", mon.Name,
		"monitor_type", mon.Type,
		"duration", time.Since(start))

	return mon.Interval, nil
}
