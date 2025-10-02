package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/queue"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

const (
	// TaskTypeHealthCheck is the task type for health check tasks
	TaskTypeHealthCheck = "monitor:healthcheck"
	// TaskTypeIngester is the task type for ingesting health check results
	TaskTypeIngester = "monitor:ingest"
)

// HealthCheckTaskPayload is the payload for health check tasks
type HealthCheckTaskPayload struct {
	MonitorID   string    `json:"monitor_id"`
	ScheduledAt time.Time `json:"scheduled_at"`
}

// IngesterTaskPayload is the payload for ingester tasks
type IngesterTaskPayload struct {
	MonitorID          string                  `json:"monitor_id"`
	MonitorName        string                  `json:"monitor_name"`
	MonitorType        string                  `json:"monitor_type"`
	MonitorInterval    int                     `json:"monitor_interval"`
	MonitorTimeout     int                     `json:"monitor_timeout"`
	MonitorMaxRetries  int                     `json:"monitor_max_retries"`
	MonitorRetryInt    int                     `json:"monitor_retry_interval"`
	MonitorResendInt   int                     `json:"monitor_resend_interval"`
	MonitorConfig      string                  `json:"monitor_config"`
	Status             heartbeat.MonitorStatus `json:"status"`
	Message            string                  `json:"message"`
	PingMs             int                     `json:"ping_ms"`
	StartTime          time.Time               `json:"start_time"`
	EndTime            time.Time               `json:"end_time"`
	IsUnderMaintenance bool                    `json:"is_under_maintenance"`
	TLSInfo            *certificate.TLSInfo    `json:"tls_info,omitempty"`
}

// HealthCheckTaskHandler handles health check tasks from the queue
type HealthCheckTaskHandler struct {
	monitorService     monitor.Service
	proxyService       proxy.Service
	execRegistry       *executor.ExecutorRegistry
	healthCheckService *healthcheck.HealthCheckSupervisor
	queueService       queue.Service
	logger             *zap.SugaredLogger
}

// NewHealthCheckTaskHandler creates a new health check task handler
func NewHealthCheckTaskHandler(
	monitorService monitor.Service,
	proxyService proxy.Service,
	execRegistry *executor.ExecutorRegistry,
	healthCheckService *healthcheck.HealthCheckSupervisor,
	queueService queue.Service,
	logger *zap.SugaredLogger,
) *HealthCheckTaskHandler {
	return &HealthCheckTaskHandler{
		monitorService:     monitorService,
		proxyService:       proxyService,
		execRegistry:       execRegistry,
		healthCheckService: healthCheckService,
		queueService:       queueService,
		logger:             logger.With("component", "healthcheck_handler"),
	}
}

// ProcessTask implements asynq.HandlerFunc
func (h *HealthCheckTaskHandler) ProcessTask(ctx context.Context, task *asynq.Task) error {
	// Parse the payload
	var payload HealthCheckTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		h.logger.Errorw("Failed to unmarshal task payload", "error", err)
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	h.logger.Debugw("Processing health check task",
		"monitor_id", payload.MonitorID,
		"scheduled_at", payload.ScheduledAt,
	)

	// Fetch the monitor
	m, err := h.monitorService.FindByID(ctx, payload.MonitorID)
	if err != nil {
		h.logger.Errorw("Failed to fetch monitor", "monitor_id", payload.MonitorID, "error", err)
		return fmt.Errorf("failed to fetch monitor: %w", err)
	}

	if m == nil {
		h.logger.Warnw("Monitor not found", "monitor_id", payload.MonitorID)
		return fmt.Errorf("monitor not found: %s", payload.MonitorID)
	}

	// Fetch proxy if needed
	var proxyModel *proxy.Model = nil
	if m.ProxyId != "" {
		p, err := h.proxyService.FindByID(ctx, m.ProxyId)
		if err != nil {
			h.logger.Errorw("Failed to fetch proxy for monitor", "monitor_id", m.ID, "proxy_id", m.ProxyId, "error", err)
		} else if p != nil {
			proxyModel = p
		}
	}

	// Get the appropriate executor for this monitor type
	exec, ok := h.execRegistry.GetExecutor(m.Type)
	if !ok {
		h.logger.Errorw("Executor not found for monitor type", "monitor_type", m.Type)
		return fmt.Errorf("executor not found for monitor type: %s", m.Type)
	}

	// Execute the health check using the supervisor's method
	tickResult := h.healthCheckService.HandleMonitorTick(ctx, m, exec, proxyModel)

	if tickResult == nil {
		h.logger.Warnw("Health check returned nil result", "monitor_id", payload.MonitorID)
		return fmt.Errorf("health check returned nil result")
	}

	h.logger.Debugw("Health check executed",
		"monitor_id", payload.MonitorID,
		"monitor_name", m.Name,
		"status", tickResult.ExecutionResult.Status,
		"ping_ms", tickResult.PingMs,
	)

	// Enqueue the result to the ingester queue
	ingesterPayload := IngesterTaskPayload{
		MonitorID:          m.ID,
		MonitorName:        m.Name,
		MonitorType:        m.Type,
		MonitorInterval:    m.Interval,
		MonitorTimeout:     m.Timeout,
		MonitorMaxRetries:  m.MaxRetries,
		MonitorRetryInt:    m.RetryInterval,
		MonitorResendInt:   m.ResendInterval,
		MonitorConfig:      m.Config,
		Status:             tickResult.ExecutionResult.Status,
		Message:            tickResult.ExecutionResult.Message,
		PingMs:             tickResult.PingMs,
		StartTime:          tickResult.ExecutionResult.StartTime,
		EndTime:            tickResult.ExecutionResult.EndTime,
		IsUnderMaintenance: tickResult.IsUnderMaintenance,
		TLSInfo:            tickResult.ExecutionResult.TLSInfo,
	}

	opts := &queue.EnqueueOptions{
		Queue:     "ingester",
		MaxRetry:  3,
		Timeout:   2 * time.Minute,
		Retention: 1 * time.Hour,
	}

	_, err = h.queueService.Enqueue(ctx, TaskTypeIngester, ingesterPayload, opts)
	if err != nil {
		h.logger.Errorw("Failed to enqueue ingester task",
			"monitor_id", payload.MonitorID,
			"error", err,
		)
		return fmt.Errorf("failed to enqueue ingester task: %w", err)
	}

	h.logger.Debugw("Successfully enqueued result to ingester",
		"monitor_id", payload.MonitorID,
		"monitor_name", m.Name,
	)

	return nil
}
