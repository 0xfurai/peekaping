package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/healthcheck/supervisor"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/shared"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

const (
	// TaskTypeHealthCheck is the task type for health check tasks
	TaskTypeHealthCheck = "monitor:healthcheck"
)

// ingesterEnqueueGrace is the time left after a check gives up for its result
// to be enqueued to the ingester before asynq cancels the task.
const ingesterEnqueueGrace = 8 * time.Second

// HealthCheckTaskTimeout returns the asynq task timeout for a check with the given
// timeout in seconds. It must outlast the check itself: when asynq's deadline fires
// first, the task fails and gets archived instead of reporting DOWN.
func HealthCheckTaskTimeout(checkTimeoutSeconds int) time.Duration {
	return time.Duration(checkTimeoutSeconds)*time.Second + supervisor.ExecutorGrace + ingesterEnqueueGrace
}

// ProxyData contains proxy configuration for health checks
type ProxyData struct {
	ID       string `json:"id"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Auth     bool   `json:"auth"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// HealthCheckTaskPayload is the payload for health check tasks
type HealthCheckTaskPayload struct {
	MonitorID      string `json:"monitor_id"`
	MonitorName    string `json:"monitor_name"`
	MonitorType    string `json:"monitor_type"`
	Interval       int    `json:"interval"`
	Timeout        int    `json:"timeout"`
	MaxRetries     int    `json:"max_retries"`
	RetryInterval  int    `json:"retry_interval"`
	ResendInterval int    `json:"resend_interval"`
	// LocationRule and LocationThreshold are the monitor's location rule, which the result of the
	// check carries to the ingester. A task queued before they existed has none, which is any.
	LocationRule       shared.LocationRule    `json:"location_rule,omitempty"`
	LocationThreshold  int                    `json:"location_threshold,omitempty"`
	Config             string                 `json:"config"`
	Proxy              *ProxyData             `json:"proxy,omitempty"`
	LastHeartbeat      *shared.HeartBeatModel `json:"last_heartbeat,omitempty"`
	ScheduledAt        time.Time              `json:"scheduled_at"`
	IsUnderMaintenance bool                   `json:"is_under_maintenance"`
	CheckCertExpiry    bool                   `json:"check_cert_expiry"`
}

// HealthCheckTaskHandler handles health check tasks from the queue
type HealthCheckTaskHandler struct {
	execRegistry       *executor.ExecutorRegistry
	healthCheckService *supervisor.HealthCheckSupervisor
	ingesterService    ingester.Service
	logger             *zap.SugaredLogger
}

// NewHealthCheckTaskHandler creates a new health check task handler. The checks' results go to
// the ingester service, which in cluster mode enqueues them for the ingester
// (ingester.NewQueueService).
func NewHealthCheckTaskHandler(
	execRegistry *executor.ExecutorRegistry,
	healthCheckService *supervisor.HealthCheckSupervisor,
	ingesterService ingester.Service,
	logger *zap.SugaredLogger,
) *HealthCheckTaskHandler {
	return &HealthCheckTaskHandler{
		execRegistry:       execRegistry,
		healthCheckService: healthCheckService,
		ingesterService:    ingesterService,
		logger:             logger.With("component", "healthcheck_handler"),
	}
}

// ProcessTask implements asynq.HandlerFunc
func (h *HealthCheckTaskHandler) ProcessTask(ctx context.Context, task *asynq.Task) error {
	start := time.Now()
	// Parse the payload
	var payload HealthCheckTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		h.logger.Errorw("Failed to unmarshal task payload", "error", err)
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	h.logger.Debugw("Processing health check task",
		"monitor_id", payload.MonitorID,
		"monitor_name", payload.MonitorName,
		"scheduled_at", payload.ScheduledAt,
	)

	// Check if task is stale and should be skipped
	now := time.Now().UTC()
	scheduledAt := payload.ScheduledAt
	timeSinceScheduled := now.Sub(scheduledAt)
	intervalDuration := time.Duration(payload.Interval) * time.Second

	// Consider task stale if it's more than 1.5x the interval old
	// This allows some buffer for processing delays while preventing old backlog processing
	staleThreshold := intervalDuration + (intervalDuration / 2)

	if timeSinceScheduled > staleThreshold {
		h.logger.Warnw("Skipping stale health check task",
			"monitor_id", payload.MonitorID,
			"monitor_name", payload.MonitorName,
			"scheduled_at", scheduledAt,
			"time_since_scheduled", timeSinceScheduled,
			"stale_threshold", staleThreshold,
			"interval", intervalDuration,
		)
		// Return nil to mark task as successfully processed (not retried)
		return nil
	}

	// Create monitor model from payload
	m := &monitor.Model{
		ID:             payload.MonitorID,
		Type:           payload.MonitorType,
		Name:           payload.MonitorName,
		Interval:       payload.Interval,
		Timeout:        payload.Timeout,
		MaxRetries:     payload.MaxRetries,
		RetryInterval:  payload.RetryInterval,
		ResendInterval: payload.ResendInterval,
		LocationRule:   payload.LocationRule,
		Config:         payload.Config,
		LastHeartbeat:  payload.LastHeartbeat,
	}
	if payload.LocationThreshold > 0 {
		m.LocationThreshold = &payload.LocationThreshold
	}

	// Create proxy model from payload if present
	var proxyModel *proxy.Model = nil
	if payload.Proxy != nil {
		proxyModel = &proxy.Model{
			ID:       payload.Proxy.ID,
			Protocol: payload.Proxy.Protocol,
			Host:     payload.Proxy.Host,
			Port:     payload.Proxy.Port,
			Auth:     payload.Proxy.Auth,
			Username: payload.Proxy.Username,
			Password: payload.Proxy.Password,
		}
	}

	// Get the appropriate executor for this monitor type
	exec, ok := h.execRegistry.GetExecutor(m.Type)
	if !ok {
		h.logger.Errorw("Executor not found for monitor type", "monitor_type", m.Type)
		return fmt.Errorf("executor not found for monitor type: %s", m.Type)
	}

	// Execute the health check using the supervisor's method
	tickResult := h.healthCheckService.HandleMonitorTick(ctx, m, exec, proxyModel, payload.IsUnderMaintenance)

	// The task was cancelled (e.g. the worker is shutting down and asynq puts it
	// back in the queue). A check cut short that way says nothing about the target.
	if errors.Is(ctx.Err(), context.Canceled) {
		return ctx.Err()
	}

	// Handle nil result (for monitors that return nil from executor)
	if tickResult == nil {
		h.logger.Debugw("Executor returned nil - no heartbeat needed",
			"monitor_id", payload.MonitorID,
			"monitor_type", m.Type)

		return nil
	}

	h.logger.Debugw("Health check executed",
		"monitor_id", payload.MonitorID,
		"monitor_name", payload.MonitorName,
		"status", tickResult.ExecutionResult.Status,
		"ping_ms", tickResult.PingMs,
	)

	// Enqueue the result to the ingester queue
	err := h.ingesterService.Ingest(ctx, tickResult.IngestResult(payload.CheckCertExpiry))
	if err != nil {
		h.logger.Errorw("Failed to enqueue ingester task",
			"monitor_id", payload.MonitorID,
			"error", err,
		)
		return fmt.Errorf("failed to enqueue ingester task: %w", err)
	}

	h.logger.Infow("Successfully enqueued result to ingester",
		"monitor_id", payload.MonitorID,
		"monitor_name", payload.MonitorName,
		"duration", time.Since(start),
	)

	return nil
}
