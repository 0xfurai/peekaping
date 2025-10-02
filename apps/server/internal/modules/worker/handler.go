package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/proxy"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

const (
	// TaskTypeHealthCheck is the task type for health check tasks
	TaskTypeHealthCheck = "monitor:healthcheck"
)

// HealthCheckTaskPayload is the payload for health check tasks
type HealthCheckTaskPayload struct {
	MonitorID   string    `json:"monitor_id"`
	ScheduledAt time.Time `json:"scheduled_at"`
}

// HealthCheckTaskHandler handles health check tasks from the queue
type HealthCheckTaskHandler struct {
	monitorService     monitor.Service
	proxyService       proxy.Service
	execRegistry       *executor.ExecutorRegistry
	healthCheckService *healthcheck.HealthCheckSupervisor
	logger             *zap.SugaredLogger
}

// NewHealthCheckTaskHandler creates a new health check task handler
func NewHealthCheckTaskHandler(
	monitorService monitor.Service,
	proxyService proxy.Service,
	execRegistry *executor.ExecutorRegistry,
	healthCheckService *healthcheck.HealthCheckSupervisor,
	logger *zap.SugaredLogger,
) *HealthCheckTaskHandler {
	return &HealthCheckTaskHandler{
		monitorService:     monitorService,
		proxyService:       proxyService,
		execRegistry:       execRegistry,
		healthCheckService: healthCheckService,
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
	// We pass nil for intervalUpdateCb since we're not managing intervals in the worker
	h.healthCheckService.HandleMonitorTick(ctx, m, exec, proxyModel, nil)

	h.logger.Debugw("Successfully processed health check task",
		"monitor_id", payload.MonitorID,
		"monitor_name", m.Name,
	)

	return nil
}
