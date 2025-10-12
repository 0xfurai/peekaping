package producer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/worker"
)

// runProducer is the main producer loop
func (p *Producer) runProducer(workerID int) error {
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
			p.logger.Errorw("Claim error", "worker_id", workerID, "error", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		ids := toStringSlice(idsAny)
		if len(ids) == 0 {
			// Sleep until next check
			time.Sleep(ClaimTick)
			continue
		}

		p.logger.Debugw("Claimed monitors for scheduling", "worker_id", workerID, "count", len(ids))

		// Process each claimed monitor with a timeout context
		// This ensures that claimed monitors can complete processing even during shutdown
		// Use a generous timeout to handle large batches (up to BatchClaim=1000 monitors)
		processCtx, processCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		pipe := p.rdb.Pipeline()
		for _, monitorID := range ids {
			interval, err := p.processMonitor(processCtx, monitorID, nowMs)
			if err != nil {
				p.logger.Errorw("Failed to process monitor",
					"worker_id", workerID,
					"monitor_id", monitorID,
					"error", err)
				// Don't reschedule on error - let lease expire and be reclaimed
				continue
			}

			// Skip rescheduling if interval is invalid (e.g., monitor was deleted or deactivated)
			if interval <= 0 {
				p.logger.Debugw("Skipping reschedule for monitor with invalid interval",
					"worker_id", workerID,
					"monitor_id", monitorID)
				continue
			}

			// Calculate next execution time
			next := nextAligned(time.UnixMilli(nowMs).UTC(), time.Duration(interval)*time.Second)
			pipe.Eval(processCtx, reschedLua, []string{SchedLeaseKey, SchedDueKey}, monitorID, next.UnixMilli())
		}

		if _, err := pipe.Exec(processCtx); err != nil {
			p.logger.Errorw("Resched pipeline error", "worker_id", workerID, "error", err)
		}
		processCancel()
	}
}

func (p *Producer) isUnderMaintenance(ctx context.Context, monitorID string) (bool, error) {
	maintenances, err := p.maintenanceService.GetMaintenancesByMonitorID(ctx, monitorID)
	if err != nil {
		return false, err
	}

	p.logger.Infof("Found %d maintenances for monitor %s", len(maintenances), monitorID)

	for _, m := range maintenances {
		underMaintenance, err := p.maintenanceService.IsUnderMaintenance(ctx, m)
		if err != nil {
			p.logger.Warnf("Failed to get maintenance status for maintenance %s: %v", m.ID, err)
			continue
		}

		// If any maintenance is under-maintenance, the monitor is under maintenance
		if underMaintenance {
			return true, nil
		}
	}

	return false, nil
}

// processMonitor loads monitor config and enqueues a health check task
// Returns the monitor interval (for rescheduling) and any error
func (p *Producer) processMonitor(ctx context.Context, monitorID string, nowMs int64) (int, error) {
	start := time.Now()
	// Fetch monitor from database
	mon, err := p.monitorService.FindByID(ctx, monitorID)
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

	isUnderMaintenance, err := p.isUnderMaintenance(ctx, monitorID)
	if err != nil {
		p.logger.Errorw("Failed to check if monitor is under maintenance", "monitor_id", monitorID, "error", err)
		return 0, err
	}

	// Fetch proxy if configured
	var proxyData *worker.ProxyData
	if mon.ProxyId != "" {
		proxyModel, err := p.proxyService.FindByID(ctx, mon.ProxyId)
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

	_, err = p.queueService.EnqueueUnique(ctx, worker.TaskTypeHealthCheck, payload, uniqueKey, ttl, opts)
	if err != nil {
		// Check if this is a duplicate task error (expected with high concurrency)
		errMsg := err.Error()
		if strings.Contains(errMsg, "task ID conflicts") ||
			strings.Contains(errMsg, "duplicated") ||
			strings.Contains(errMsg, "already exists") {
			// This is not an error - the task is already queued, which is exactly what we want
			// This commonly happens when multiple workers process monitors concurrently
			p.logger.Debugw("Monitor task already queued (duplicate prevented)",
				"monitor_id", mon.ID,
				"duration", time.Since(start))
			return mon.Interval, nil
		}
		// This is a real error
		return 0, fmt.Errorf("failed to enqueue health check: %w", err)
	}

	p.logger.Infow("Enqueued health check",
		"monitor_id", mon.ID,
		"monitor_name", mon.Name,
		"monitor_type", mon.Type,
		"duration", time.Since(start))

	return mon.Interval, nil
}
