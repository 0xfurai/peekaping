package ingester

import (
	"context"
	"encoding/json"
	"fmt"
	"vigi/internal/modules/certificate"
	"vigi/internal/modules/events"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/monitor_maintenance"
	"vigi/internal/modules/shared"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

const (
	// TaskTypeIngester is the task type for ingesting health check results
	TaskTypeIngester = "monitor:ingest"
)

// IngesterTaskPayload is the payload for ingester tasks
type IngesterTaskPayload struct {
	MonitorID          string               `json:"monitor_id"`
	MonitorName        string               `json:"monitor_name"`
	MonitorType        string               `json:"monitor_type"`
	MonitorInterval    int                  `json:"monitor_interval"`
	MonitorTimeout     int                  `json:"monitor_timeout"`
	MonitorMaxRetries  int                  `json:"monitor_max_retries"`
	MonitorRetryInt    int                  `json:"monitor_retry_interval"`
	MonitorResendInt   int                  `json:"monitor_resend_interval"`
	MonitorConfig      string               `json:"monitor_config"`
	Status             shared.MonitorStatus `json:"status"`
	Message            string               `json:"message"`
	PingMs             int                  `json:"ping_ms"`
	StartTime          time.Time            `json:"start_time"`
	EndTime            time.Time            `json:"end_time"`
	IsUnderMaintenance bool                 `json:"is_under_maintenance"`
	TLSInfo            *certificate.TLSInfo `json:"tls_info,omitempty"`
	CheckCertExpiry    bool                 `json:"check_cert_expiry"`
}

// IngesterTaskHandler handles ingester tasks from the queue
type IngesterTaskHandler struct {
	heartbeatService          heartbeat.Service
	certificateService        certificate.Service
	monitorMaintenanceService monitor_maintenance.Service
	eventBus                  events.EventBus
	logger                    *zap.SugaredLogger
}

// NewIngesterTaskHandler creates a new ingester task handler
func NewIngesterTaskHandler(
	heartbeatService heartbeat.Service,
	certificateService certificate.Service,
	monitorMaintenanceService monitor_maintenance.Service,
	eventBus events.EventBus,
	logger *zap.SugaredLogger,
) *IngesterTaskHandler {
	return &IngesterTaskHandler{
		heartbeatService:          heartbeatService,
		certificateService:        certificateService,
		monitorMaintenanceService: monitorMaintenanceService,
		eventBus:                  eventBus,
		logger:                    logger.With("component", "ingester_handler"),
	}
}

// ProcessTask implements asynq.HandlerFunc
func (h *IngesterTaskHandler) ProcessTask(ctx context.Context, task *asynq.Task) error {
	start := time.Now()
	// Parse the payload
	var payload IngesterTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		h.logger.Errorw("Failed to unmarshal task payload", "error", err)
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	h.logger.Debugw("Processing ingester task",
		"monitor_id", payload.MonitorID,
		"monitor_name", payload.MonitorName,
		"status", payload.Status,
	)

	// Process the heartbeat
	if err := h.processHeartbeat(ctx, &payload); err != nil {
		h.logger.Errorw("Failed to process heartbeat",
			"monitor_id", payload.MonitorID,
			"error", err,
		)
		return fmt.Errorf("failed to process heartbeat: %w", err)
	}

	h.logger.Infow("Successfully processed ingester task",
		"monitor_id", payload.MonitorID,
		"monitor_name", payload.MonitorName,
		"duration", time.Since(start),
	)

	return nil
}

// isImportantBeat checks if the beat is important (status changed)
func (h *IngesterTaskHandler) isImportantBeat(prevBeatStatus, currBeatStatus shared.MonitorStatus) bool {
	up := shared.MonitorStatusUp
	down := shared.MonitorStatusDown
	pending := shared.MonitorStatusPending

	return (prevBeatStatus == up && currBeatStatus == down) ||
		(prevBeatStatus == down && currBeatStatus == up) ||
		(prevBeatStatus == pending && currBeatStatus == up) ||
		(prevBeatStatus == pending && currBeatStatus == down)
}

// isImportantForNotification checks if the beat should trigger notification
func (h *IngesterTaskHandler) isImportantForNotification(prevBeatStatus, currBeatStatus shared.MonitorStatus) bool {
	up := shared.MonitorStatusUp
	down := shared.MonitorStatusDown
	pending := shared.MonitorStatusPending

	return (prevBeatStatus == up && currBeatStatus == down) ||
		(prevBeatStatus == down && currBeatStatus == up) ||
		(prevBeatStatus == pending && currBeatStatus == down)
}

// processHeartbeat processes and stores the heartbeat
func (h *IngesterTaskHandler) processHeartbeat(ctx context.Context, payload *IngesterTaskPayload) error {
	// Get the previous heartbeat
	previousBeats, err := h.heartbeatService.FindByMonitorIDPaginated(ctx, payload.MonitorID, 1, 0, nil, false)
	var previousBeat *heartbeat.Model = nil
	if err != nil {
		h.logger.Errorw("Failed to get previous heartbeat for monitor",
			"monitor_id", payload.MonitorID,
			"error", err,
		)
	}
	if len(previousBeats) > 0 {
		previousBeat = previousBeats[0]
	}

	isFirstBeat := previousBeat == nil

	hb := &heartbeat.CreateUpdateDto{
		MonitorID: payload.MonitorID,
		Status:    payload.Status,
		Msg:       payload.Message,
		Ping:      payload.PingMs,
		Duration:  0,
		DownCount: 0,
		Retries:   0,
		Important: false,
		Time:      payload.StartTime,
		EndTime:   payload.EndTime,
		Notified:  false,
	}

	if !isFirstBeat {
		hb.DownCount = previousBeat.DownCount
		hb.Retries = previousBeat.Retries
	}

	// Mark as pending if max retries is set and retries is less than max retries
	if payload.Status == shared.MonitorStatusDown {
		if !isFirstBeat && payload.MonitorMaxRetries > 0 && previousBeat.Retries < payload.MonitorMaxRetries {
			hb.Status = shared.MonitorStatusPending
		}
		hb.Retries++
	} else {
		hb.Retries = 0
	}

	isImportant := isFirstBeat || h.isImportantBeat(previousBeat.Status, hb.Status)
	shouldNotify := false

	// If important (beat status changed), send notification
	if isImportant {
		hb.Important = true

		if isFirstBeat || h.isImportantForNotification(previousBeat.Status, hb.Status) {
			h.logger.Debugw("Marking for notification", "monitor_name", payload.MonitorName)
			shouldNotify = true
			hb.Notified = true
		}

		hb.DownCount = 0
	} else {
		hb.Important = false

		if payload.Status == shared.MonitorStatusDown && payload.MonitorResendInt > 0 {
			hb.DownCount += 1

			if hb.DownCount >= payload.MonitorResendInt {
				shouldNotify = true
				hb.Notified = true
				hb.DownCount = 0
			}
		}
	}

	// Log status
	if payload.Status == shared.MonitorStatusUp {
		h.logger.Debugw("Monitor up",
			"monitor_name", payload.MonitorName,
			"ping_ms", payload.PingMs,
			"interval", payload.MonitorInterval,
			"type", payload.MonitorType,
		)
	} else if payload.Status == shared.MonitorStatusPending {
		h.logger.Debugw("Monitor pending",
			"monitor_name", payload.MonitorName,
			"ping_ms", payload.PingMs,
			"interval", payload.MonitorInterval,
			"type", payload.MonitorType,
		)
	} else if payload.Status == shared.MonitorStatusDown {
		h.logger.Debugw("Monitor down",
			"monitor_name", payload.MonitorName,
			"ping_ms", payload.PingMs,
			"interval", payload.MonitorInterval,
			"type", payload.MonitorType,
		)
	} else if payload.Status == shared.MonitorStatusMaintenance {
		h.logger.Debugw("Monitor under maintenance",
			"monitor_name", payload.MonitorName,
			"ping_ms", payload.PingMs,
			"interval", payload.MonitorInterval,
			"type", payload.MonitorType,
		)
	}

	// Update TLS info and check certificate expiry for HTTPS monitors
	if payload.TLSInfo != nil && strings.HasPrefix(strings.ToLower(payload.MonitorType), "http") {
		// Update TLS info (this handles certificate change detection and notification history cleanup)
		if err := h.certificateService.UpdateTLSInfo(ctx, payload.MonitorID, payload.TLSInfo); err != nil {
			h.logger.Errorw("Failed to update TLS info for monitor",
				"monitor_name", payload.MonitorName,
				"error", err,
			)
		}

		// Check certificate expiry and send notifications only if enabled (flag comes from payload)
		if payload.CheckCertExpiry {
			if err := h.certificateService.CheckCertificateExpiry(ctx, payload.TLSInfo, payload.MonitorID, payload.MonitorName); err != nil {
				h.logger.Errorw("Failed to check certificate expiry for monitor",
					"monitor_name", payload.MonitorName,
					"error", err,
				)
			}
		} else {
			h.logger.Debugw("Certificate expiry checking disabled for monitor", "monitor_name", payload.MonitorName)
		}
	}

	// Create the heartbeat in the database
	dbHb, err := h.heartbeatService.Create(ctx, hb)
	if err != nil {
		h.logger.Errorw("Failed to create heartbeat",
			"monitor_id", payload.MonitorID,
			"error", err,
		)
		return fmt.Errorf("failed to create heartbeat: %w", err)
	}

	// Publish events
	if isFirstBeat || previousBeat.Status != hb.Status {
		h.eventBus.Publish(events.Event{
			Type:    events.MonitorStatusChanged,
			Payload: dbHb,
		})
	}

	if shouldNotify {
		h.eventBus.Publish(events.Event{
			Type:    events.ImportantHeartbeat,
			Payload: dbHb,
		})
	}

	return nil
}
