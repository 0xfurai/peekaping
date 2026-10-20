package ingester

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

const (
	// TaskTypeIngester is the task type for ingesting health check results
	TaskTypeIngester = "monitor:ingest"
)

// IngesterTaskHandler handles ingester tasks from the queue
type IngesterTaskHandler struct {
	service Service
	logger  *zap.SugaredLogger
}

// NewIngesterTaskHandler creates a new ingester task handler
func NewIngesterTaskHandler(
	service Service,
	logger *zap.SugaredLogger,
) *IngesterTaskHandler {
	return &IngesterTaskHandler{
		service: service,
		logger:  logger.With("component", "ingester_handler"),
	}
}

// ProcessTask implements asynq.HandlerFunc
func (h *IngesterTaskHandler) ProcessTask(ctx context.Context, task *asynq.Task) error {
	start := time.Now()
	// Parse the payload
	var result Result
	if err := json.Unmarshal(task.Payload(), &result); err != nil {
		h.logger.Errorw("Failed to unmarshal task payload", "error", err)
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	h.logger.Debugw("Processing ingester task",
		"monitor_id", result.MonitorID,
		"monitor_name", result.MonitorName,
		"status", result.Status,
	)

	// Process the heartbeat
	if err := h.service.Ingest(ctx, &result); err != nil {
		h.logger.Errorw("Failed to process heartbeat",
			"monitor_id", result.MonitorID,
			"error", err,
		)
		return fmt.Errorf("failed to process heartbeat: %w", err)
	}

	h.logger.Infow("Successfully processed ingester task",
		"monitor_id", result.MonitorID,
		"monitor_name", result.MonitorName,
		"duration", time.Since(start),
	)

	return nil
}
