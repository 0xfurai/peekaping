package producer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/worker"
)

const healthCheckQueue = "healthcheck"

// healthCheckTaskID is the asynq task ID of a monitor's health check. It is fixed
// per monitor, so the monitor's queued check can be found and removed directly.
func healthCheckTaskID(monitorID string) string {
	return "healthcheck:" + monitorID
}

// claimDueMonitors atomically claims a batch of due monitors from the due queue
// It moves monitors from the due set (where score <= nowMs) to the lease set with a lease expiry
func (p *Producer) claimDueMonitors(ctx context.Context, nowMs int64, maxMonitors int, leaseTTLMs int64) ([]string, error) {
	result, err := claimScript.Run(
		ctx,
		p.rdb,
		[]string{SchedDueKey, SchedLeaseKey},
		nowMs,
		maxMonitors,
		leaseTTLMs,
	).Result()
	if err != nil {
		return nil, err
	}
	return toStringSlice(result), nil
}

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
		leaseTTLMs := int64(LeaseTTL / time.Millisecond)

		// Atomically claim a batch of due monitors
		ids, err := p.claimDueMonitors(p.ctx, nowMs, BatchClaim, leaseTTLMs)
		if err != nil {
			p.logger.Errorw("Claim error", "worker_id", workerID, "error", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// If no monitors were claimed, sleep until next check
		if len(ids) == 0 {
			// Sleep until next check
			time.Sleep(ClaimTick)
			continue
		}

		p.logger.Debugw("Claimed monitors for scheduling", "worker_id", workerID, "count", len(ids))

		// Process each claimed monitor with a timeout context
		// This ensures that claimed monitors can complete processing even during shutdown
		// Use a generous timeout to handle large batches (up to BatchClaim=1000 monitors)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		pipe := p.rdb.Pipeline()
		for _, monitorID := range ids {
			interval, err := p.processMonitor(ctx, monitorID, nowMs)
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
			pipe.Eval(
				ctx,
				reschedLua,
				[]string{SchedLeaseKey, SchedDueKey},
				monitorID,
				next.UnixMilli(),
			)
		}

		if _, err := pipe.Exec(ctx); err != nil {
			p.logger.Errorw("Resched pipeline error", "worker_id", workerID, "error", err)
		}
		cancel()
	}
}

func (p *Producer) isUnderMaintenance(ctx context.Context, monitorID string) (bool, error) {
	maintenances, err := p.maintenanceService.GetMaintenancesByMonitorID(ctx, monitorID)
	if err != nil {
		return false, err
	}

	p.logger.Debugf("Found %d maintenances for monitor %s", len(maintenances), monitorID)

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

	// Check if certificate expiry checking is enabled in monitor configuration
	// This applies to monitors that support TLS (http, tcp)
	checkCertExpiry := false
	monType := strings.ToLower(mon.Type)
	if strings.HasPrefix(monType, "http") || monType == "tcp" {
		if mon.Config != "" {
			// Parse monitor configuration to check if certificate expiry checking is enabled
			var config struct {
				CheckCertExpiry bool `json:"check_cert_expiry"`
			}
			if err := json.Unmarshal([]byte(mon.Config), &config); err != nil {
				p.logger.Warnw("Failed to parse monitor config for certificate expiry check",
					"monitor_id", mon.ID,
					"monitor_name", mon.Name,
					"error", err)
			} else {
				checkCertExpiry = config.CheckCertExpiry
			}
		}
		p.logger.Debugw("Certificate expiry checking configured",
			"monitor_id", mon.ID,
			"monitor_name", mon.Name,
			"check_cert_expiry", checkCertExpiry)
	}

	// For push monitors, fetch the latest heartbeat to include in the payload
	var lastHeartbeat *shared.HeartBeatModel
	if mon.Type == "push" {
		latestHeartbeats, err := p.heartbeatService.FindByMonitorIDPaginated(ctx, mon.ID, 1, 0, nil, false)
		if err != nil {
			p.logger.Warnw("Failed to fetch latest heartbeat for push monitor",
				"monitor_id", mon.ID,
				"error", err)
		} else if len(latestHeartbeats) > 0 {
			lastHeartbeat = latestHeartbeats[0]
		}
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
		LastHeartbeat:      lastHeartbeat,
		ScheduledAt:        time.UnixMilli(nowMs).UTC(),
		IsUnderMaintenance: isUnderMaintenance,
		CheckCertExpiry:    checkCertExpiry,
	}

	// Enqueue task to worker queue
	opts := &queue.EnqueueOptions{
		Queue:     healthCheckQueue,
		MaxRetry:  0,
		Timeout:   worker.HealthCheckTaskTimeout(mon.Timeout),
		Retention: 0,
		TaskID:    healthCheckTaskID(mon.ID),
	}

	enqueued, err := p.enqueueHealthCheck(ctx, mon.Interval, nowMs, payload, opts)
	if err != nil {
		return 0, fmt.Errorf("failed to enqueue health check: %w", err)
	}
	if !enqueued {
		p.logger.Debugw("Previous health check still in flight, skipping",
			"monitor_id", mon.ID,
			"duration", time.Since(start))
		return mon.Interval, nil
	}

	p.logger.Infow("Enqueued health check",
		"monitor_id", mon.ID,
		"monitor_name", mon.Name,
		"monitor_type", mon.Type,
		"duration", time.Since(start))

	return mon.Interval, nil
}

// enqueueHealthCheck enqueues a check under the monitor's fixed task ID, which keeps
// at most one check per monitor in the queue. asynq rejects the ID while any task
// holds it, archived ones included, so a leftover task is deleted and replaced
// instead of blocking the monitor. It returns false when the previous check is
// still in flight.
func (p *Producer) enqueueHealthCheck(
	ctx context.Context,
	interval int,
	nowMs int64,
	payload worker.HealthCheckTaskPayload,
	opts *queue.EnqueueOptions,
) (bool, error) {
	_, err := p.queueService.Enqueue(ctx, worker.TaskTypeHealthCheck, payload, opts)
	if !errors.Is(err, queue.ErrTaskIDConflict) {
		return err == nil, err
	}

	existing, err := p.queueService.GetTaskInfo(ctx, healthCheckQueue, opts.TaskID)
	if err != nil {
		// Most likely the task finished between the two calls; the next tick enqueues.
		p.logger.Debugw("Failed to look up conflicting health check task", "task_id", opts.TaskID, "error", err)
		return false, nil
	}
	if healthCheckInFlight(existing, interval, nowMs) {
		return false, nil
	}

	// DeleteTask refuses active tasks, so a check that has just started is left alone.
	if err := p.queueService.DeleteTask(ctx, healthCheckQueue, opts.TaskID); err != nil {
		p.logger.Warnw("Failed to remove leftover health check task", "task_id", opts.TaskID, "error", err)
		return false, nil
	}
	p.logger.Warnw("Replaced leftover health check task",
		"task_id", opts.TaskID,
		"state", existing.State,
		"last_error", existing.LastErr)

	_, err = p.queueService.Enqueue(ctx, worker.TaskTypeHealthCheck, payload, opts)
	if errors.Is(err, queue.ErrTaskIDConflict) {
		return false, nil
	}
	return err == nil, err
}

// healthCheckInFlight reports whether a task holding a monitor's task ID is a check
// that is running or about to run. Anything else is a leftover: archived after a
// failure, or pending for over an interval with a possibly outdated config.
func healthCheckInFlight(task *queue.TaskInfo, interval int, nowMs int64) bool {
	switch task.State {
	case queue.TaskStateActive:
		return true
	case queue.TaskStatePending:
		var payload worker.HealthCheckTaskPayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			return false
		}
		age := time.UnixMilli(nowMs).Sub(payload.ScheduledAt)
		return age < time.Duration(interval)*time.Second
	default:
		return false
	}
}
