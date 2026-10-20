package producer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"peekaping/internal/modules/check"
	"peekaping/internal/modules/queue"
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

// processMonitor loads monitor config and enqueues a health check task
// Returns the monitor interval (for rescheduling) and any error
func (p *Producer) processMonitor(ctx context.Context, monitorID string, nowMs int64) (int, error) {
	start := time.Now()
	job, err := p.loader.Load(ctx, monitorID)
	if errors.Is(err, check.ErrMonitorNotFound) {
		p.logger.Warnw("Monitor not found, skipping", "monitor_id", monitorID)
		return 0, nil
	}
	if errors.Is(err, check.ErrMonitorInactive) {
		p.logger.Infow("Skipping inactive monitor", "monitor_id", monitorID)
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	mon := job.Monitor

	var proxyData *worker.ProxyData
	if job.Proxy != nil {
		proxyData = &worker.ProxyData{
			ID:       job.Proxy.ID,
			Protocol: job.Proxy.Protocol,
			Host:     job.Proxy.Host,
			Port:     job.Proxy.Port,
			Auth:     job.Proxy.Auth,
			Username: job.Proxy.Username,
			Password: job.Proxy.Password,
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
		LastHeartbeat:      job.LastHeartbeat,
		ScheduledAt:        time.UnixMilli(nowMs).UTC(),
		IsUnderMaintenance: job.IsUnderMaintenance,
		CheckCertExpiry:    job.CheckCertExpiry,
	}

	// Enqueue task to worker queue
	opts := &queue.EnqueueOptions{
		Queue:     healthCheckQueue,
		MaxRetry:  0,
		Timeout:   worker.HealthCheckTaskTimeout(mon.Timeout),
		Retention: 0,
		TaskID:    healthCheckTaskID(mon.ID),
	}

	enqueued, err := p.enqueueHealthCheck(ctx, payload, opts)
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
	payload worker.HealthCheckTaskPayload,
	opts *queue.EnqueueOptions,
) (bool, error) {
	_, err := p.queueService.Enqueue(ctx, worker.TaskTypeHealthCheck, payload, opts)
	if !errors.Is(err, queue.ErrTaskIDConflict) {
		return err == nil, err
	}

	existing, err := p.queueService.GetTaskInfo(ctx, healthCheckQueue, opts.TaskID)
	if errors.Is(err, queue.ErrTaskNotFound) {
		// It finished between the two calls; the next tick enqueues
		return false, nil
	}
	if err != nil {
		p.logger.Warnw("Failed to look up conflicting health check task", "task_id", opts.TaskID, "error", err)
		return false, nil
	}
	// A pending or running check still reports on its own. Pending ones are not
	// replaced even when old: that would send them to the back of a backlogged queue.
	if existing.State == queue.TaskStateActive || existing.State == queue.TaskStatePending {
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
