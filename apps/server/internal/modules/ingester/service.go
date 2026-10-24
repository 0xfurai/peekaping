package ingester

import (
	"context"
	"fmt"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/shared"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Result is the outcome of one check, or of one push to a push monitor, with the monitor
// settings the ingest logic reads. It is also the JSON payload of the ingester task: the
// worker, the API's push URL and the probe protocol's results route encode it, and the ingester
// decodes it.
type Result struct {
	// ProbeID is the probe that ran the check, stored on the heartbeat. The local worker and the
	// push URL set it to the local probe; a task queued before they did leaves it empty, which
	// the ingester also takes as the local probe.
	ProbeID            string               `json:"probe_id,omitempty"`
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

// Service turns check results into heartbeats: ADR 0002's "ingest a result". NewService stores
// them. In cluster mode the worker and the API's push URL get NewQueueService and
// NewPushQueueService, which enqueue the results for the ingester, and the ingester stores
// them with NewService. Single mode's checks and pushes call the same NewInOrderService.
type Service interface {
	// Ingest stores the result as a heartbeat and publishes the events that follow from it, or
	// hands it to the ingester that does. Storing reads the monitor's previous heartbeat to
	// decide Pending, Down and whether to notify, so the results of one monitor must be
	// stored one at a time, in order.
	Ingest(ctx context.Context, r *Result) error
}

type ServiceImpl struct {
	heartbeatService   heartbeat.Service
	certificateService certificate.Service
	eventBus           events.EventBus
	logger             *zap.SugaredLogger
}

// NewService creates the service that stores check results
func NewService(
	heartbeatService heartbeat.Service,
	certificateService certificate.Service,
	eventBus events.EventBus,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		heartbeatService:   heartbeatService,
		certificateService: certificateService,
		eventBus:           eventBus,
		logger:             logger.With("component", "ingester_service"),
	}
}

// isImportantBeat checks if the beat is important (status changed)
func (s *ServiceImpl) isImportantBeat(prevBeatStatus, currBeatStatus shared.MonitorStatus) bool {
	up := shared.MonitorStatusUp
	down := shared.MonitorStatusDown
	pending := shared.MonitorStatusPending

	return (prevBeatStatus == up && currBeatStatus == down) ||
		(prevBeatStatus == down && currBeatStatus == up) ||
		(prevBeatStatus == pending && currBeatStatus == up) ||
		(prevBeatStatus == pending && currBeatStatus == down)
}

// isImportantForNotification checks if the beat should trigger notification
func (s *ServiceImpl) isImportantForNotification(prevBeatStatus, currBeatStatus shared.MonitorStatus) bool {
	up := shared.MonitorStatusUp
	down := shared.MonitorStatusDown
	pending := shared.MonitorStatusPending

	return (prevBeatStatus == up && currBeatStatus == down) ||
		(prevBeatStatus == down && currBeatStatus == up) ||
		(prevBeatStatus == pending && currBeatStatus == down)
}

// Ingest stores the result as a heartbeat and publishes the events that follow from it
func (s *ServiceImpl) Ingest(ctx context.Context, r *Result) error {
	// Get the previous heartbeat
	previousBeats, err := s.heartbeatService.FindByMonitorIDPaginated(ctx, r.MonitorID, 1, 0, nil, false)
	var previousBeat *heartbeat.Model = nil
	if err != nil {
		s.logger.Errorw("Failed to get previous heartbeat for monitor",
			"monitor_id", r.MonitorID,
			"error", err,
		)
	}
	if len(previousBeats) > 0 {
		previousBeat = previousBeats[0]
	}

	isFirstBeat := previousBeat == nil

	probeID := r.ProbeID
	if probeID == "" {
		probeID = shared.LocalProbeID
	}

	hb := &heartbeat.CreateUpdateDto{
		MonitorID: r.MonitorID,
		ProbeID:   probeID,
		Status:    r.Status,
		Msg:       r.Message,
		Ping:      r.PingMs,
		Duration:  0,
		DownCount: 0,
		Retries:   0,
		Important: false,
		Time:      r.StartTime,
		EndTime:   r.EndTime,
		Notified:  false,
	}

	if !isFirstBeat {
		hb.DownCount = previousBeat.DownCount
		hb.Retries = previousBeat.Retries
	}

	// Mark as pending if max retries is set and retries is less than max retries
	if r.Status == shared.MonitorStatusDown {
		if !isFirstBeat && r.MonitorMaxRetries > 0 && previousBeat.Retries < r.MonitorMaxRetries {
			hb.Status = shared.MonitorStatusPending
		}
		hb.Retries++
	} else {
		hb.Retries = 0
	}

	isImportant := isFirstBeat || s.isImportantBeat(previousBeat.Status, hb.Status)
	shouldNotify := false

	// If important (beat status changed), send notification
	if isImportant {
		hb.Important = true

		if isFirstBeat || s.isImportantForNotification(previousBeat.Status, hb.Status) {
			s.logger.Debugw("Marking for notification", "monitor_name", r.MonitorName)
			shouldNotify = true
			hb.Notified = true
		}

		hb.DownCount = 0
	} else {
		hb.Important = false

		if r.Status == shared.MonitorStatusDown && r.MonitorResendInt > 0 {
			hb.DownCount += 1

			if hb.DownCount >= r.MonitorResendInt {
				shouldNotify = true
				hb.Notified = true
				hb.DownCount = 0
			}
		}
	}

	// Log status
	if r.Status == shared.MonitorStatusUp {
		s.logger.Debugw("Monitor up",
			"monitor_name", r.MonitorName,
			"ping_ms", r.PingMs,
			"interval", r.MonitorInterval,
			"type", r.MonitorType,
		)
	} else if r.Status == shared.MonitorStatusPending {
		s.logger.Debugw("Monitor pending",
			"monitor_name", r.MonitorName,
			"ping_ms", r.PingMs,
			"interval", r.MonitorInterval,
			"type", r.MonitorType,
		)
	} else if r.Status == shared.MonitorStatusDown {
		s.logger.Debugw("Monitor down",
			"monitor_name", r.MonitorName,
			"ping_ms", r.PingMs,
			"interval", r.MonitorInterval,
			"type", r.MonitorType,
		)
	} else if r.Status == shared.MonitorStatusMaintenance {
		s.logger.Debugw("Monitor under maintenance",
			"monitor_name", r.MonitorName,
			"ping_ms", r.PingMs,
			"interval", r.MonitorInterval,
			"type", r.MonitorType,
		)
	}

	// Update TLS info and check certificate expiry for HTTPS and SMTP monitors
	monitorType := strings.ToLower(r.MonitorType)
	supportsTLS := strings.HasPrefix(monitorType, "http") || monitorType == "smtp"
	if r.TLSInfo != nil && supportsTLS {
		// Update TLS info (this handles certificate change detection and notification history cleanup)
		if err := s.certificateService.UpdateTLSInfo(ctx, r.MonitorID, r.TLSInfo); err != nil {
			s.logger.Errorw("Failed to update TLS info for monitor",
				"monitor_name", r.MonitorName,
				"error", err,
			)
		}

		// Check certificate expiry and send notifications only if enabled (the flag comes from the result)
		if r.CheckCertExpiry {
			if err := s.certificateService.CheckCertificateExpiry(ctx, r.TLSInfo, r.MonitorID, r.MonitorName); err != nil {
				s.logger.Errorw("Failed to check certificate expiry for monitor",
					"monitor_name", r.MonitorName,
					"error", err,
				)
			}
		} else {
			s.logger.Debugw("Certificate expiry checking disabled for monitor", "monitor_name", r.MonitorName)
		}
	}

	// Create the heartbeat in the database
	dbHb, err := s.heartbeatService.Create(ctx, hb)
	if err != nil {
		s.logger.Errorw("Failed to create heartbeat",
			"monitor_id", r.MonitorID,
			"error", err,
		)
		return fmt.Errorf("failed to create heartbeat: %w", err)
	}

	// Publish events
	if isFirstBeat || previousBeat.Status != hb.Status {
		if err := events.Publish(ctx, s.eventBus, events.MonitorStatusChanged, *dbHb); err != nil {
			s.logger.Errorw("Failed to publish monitor status changed event", "monitor_id", dbHb.MonitorID, "error", err)
		}
	}

	if shouldNotify {
		if err := events.Publish(ctx, s.eventBus, events.ImportantHeartbeat, *dbHb); err != nil {
			s.logger.Errorw("Failed to publish important heartbeat event", "monitor_id", dbHb.MonitorID, "error", err)
		}
	}

	return nil
}
