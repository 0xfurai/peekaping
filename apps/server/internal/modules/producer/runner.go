package producer

import (
	"context"
	"errors"
	"time"

	"peekaping/internal/modules/check"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/worker"

	"go.uber.org/zap"
)

const healthCheckQueue = "healthcheck"

// healthCheckTaskID is the asynq task ID of a monitor's health check. It is fixed
// per monitor, so the monitor's queued check can be found and removed directly.
func healthCheckTaskID(monitorID string) string {
	return "healthcheck:" + monitorID
}

// TaskQueue is the part of the queue service the queue runner uses
type TaskQueue interface {
	Enqueue(ctx context.Context, taskType string, payload interface{}, opts *queue.EnqueueOptions) (*queue.TaskInfo, error)
	GetTaskInfo(ctx context.Context, queueName, taskID string) (*queue.TaskInfo, error)
	DeleteTask(ctx context.Context, queueName, taskID string) error
	CancelTask(ctx context.Context, taskID string) error
}

// queueRunner runs checks by enqueueing them as health check tasks, which cluster mode's
// worker takes from the healthcheck queue and runs
type queueRunner struct {
	queueService TaskQueue
	logger       *zap.SugaredLogger
}

// NewQueueRunner creates cluster mode's check.Runner, which enqueues each check for the worker
func NewQueueRunner(queueService TaskQueue, logger *zap.SugaredLogger) check.Runner {
	return &queueRunner{
		queueService: queueService,
		logger:       logger.With("component", "producer"),
	}
}

// Run enqueues the job's check under the monitor's fixed task ID. It reports false, and
// enqueues nothing, while the monitor's previous check is still queued or running.
func (r *queueRunner) Run(ctx context.Context, job *check.Job, scheduledAt time.Time) (bool, error) {
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
		LocationRule:       mon.LocationRule,
		LocationThreshold:  mon.LocationThresholdValue(),
		Config:             mon.Config,
		Proxy:              proxyData,
		LastHeartbeat:      job.LastHeartbeat,
		ScheduledAt:        scheduledAt,
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

	return r.enqueue(ctx, payload, opts)
}

// enqueue enqueues a check under the monitor's fixed task ID, which keeps
// at most one check per monitor in the queue. asynq rejects the ID while any task
// holds it, archived ones included, so a leftover task is deleted and replaced
// instead of blocking the monitor. It returns false when the previous check is
// still in flight.
func (r *queueRunner) enqueue(
	ctx context.Context,
	payload worker.HealthCheckTaskPayload,
	opts *queue.EnqueueOptions,
) (bool, error) {
	_, err := r.queueService.Enqueue(ctx, worker.TaskTypeHealthCheck, payload, opts)
	if !errors.Is(err, queue.ErrTaskIDConflict) {
		return err == nil, err
	}

	existing, err := r.queueService.GetTaskInfo(ctx, healthCheckQueue, opts.TaskID)
	if errors.Is(err, queue.ErrTaskNotFound) {
		// It finished between the two calls; the next tick enqueues
		return false, nil
	}
	if err != nil {
		r.logger.Warnw("Failed to look up conflicting health check task", "task_id", opts.TaskID, "error", err)
		return false, nil
	}
	// A pending or running check still reports on its own. Pending ones are not
	// replaced even when old: that would send them to the back of a backlogged queue.
	if existing.State == queue.TaskStateActive || existing.State == queue.TaskStatePending {
		return false, nil
	}

	// DeleteTask refuses active tasks, so a check that has just started is left alone.
	if err := r.queueService.DeleteTask(ctx, healthCheckQueue, opts.TaskID); err != nil {
		r.logger.Warnw("Failed to remove leftover health check task", "task_id", opts.TaskID, "error", err)
		return false, nil
	}
	r.logger.Warnw("Replaced leftover health check task",
		"task_id", opts.TaskID,
		"state", existing.State,
		"last_error", existing.LastErr)

	_, err = r.queueService.Enqueue(ctx, worker.TaskTypeHealthCheck, payload, opts)
	if errors.Is(err, queue.ErrTaskIDConflict) {
		return false, nil
	}
	return err == nil, err
}

// Cancel removes a monitor's queued health check and cancels a
// running one, so that no result of the old config is recorded and the next
// check runs with the monitor's current config.
func (r *queueRunner) Cancel(ctx context.Context, monitorID string) {
	taskID := healthCheckTaskID(monitorID)
	task, err := r.queueService.GetTaskInfo(ctx, healthCheckQueue, taskID)
	if errors.Is(err, queue.ErrTaskNotFound) {
		return
	}
	if err != nil {
		r.logger.Warnw("Failed to look up queued health check", "task_id", taskID, "error", err)
		return
	}
	if task.State != queue.TaskStateActive {
		err := r.queueService.DeleteTask(ctx, healthCheckQueue, taskID)
		if err == nil || errors.Is(err, queue.ErrTaskNotFound) {
			return
		}
		// A worker may have picked the task up since the lookup
		task, lookupErr := r.queueService.GetTaskInfo(ctx, healthCheckQueue, taskID)
		if lookupErr != nil || task.State != queue.TaskStateActive {
			r.logger.Warnw("Failed to purge queued health check", "task_id", taskID, "error", err)
			return
		}
	}
	// The worker drops the result of a cancelled check, and the task it leaves
	// archived is replaced by the next enqueue
	if err := r.queueService.CancelTask(ctx, taskID); err != nil {
		r.logger.Warnw("Failed to cancel running health check", "task_id", taskID, "error", err)
	}
}
