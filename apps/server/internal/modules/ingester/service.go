package ingester

import (
	"context"
	"fmt"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/shared"
	"slices"
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
	// hands it to the ingester that does. Storing reads the previous heartbeat of the monitor on
	// the probe that ran the check to decide Pending and Down, and the state of the monitor's
	// other probes to decide whether the monitor changed status, notify and so on. The results of
	// one probe of a monitor must be stored one at a time, in order; those of different probes
	// of one monitor are ordered by LocationStore.
	Ingest(ctx context.Context, r *Result) error
}

// LocationStore keeps a monitor's state on each probe it runs on, which the ingester reads and
// writes to judge the monitor as a whole. The monitor repository is one.
type LocationStore interface {
	// RecordLocation stores what the result left on the monitor's row for the probe, and returns
	// the monitor's rows as they were just before and just after, in probe id order. It makes
	// two results of one monitor, recorded at once, see one another in turn. A monitor without a
	// row for the probe has nothing stored, and a monitor without rows (MongoDB's, or one from
	// before probes) returns none.
	RecordLocation(ctx context.Context, monitorID string, state shared.LocationState) (before, after []shared.MonitorLocation, err error)
}

type ServiceImpl struct {
	heartbeatService   heartbeat.Service
	certificateService certificate.Service
	locations          LocationStore
	eventBus           events.EventBus
	logger             *zap.SugaredLogger
}

// NewService creates the service that stores check results
func NewService(
	heartbeatService heartbeat.Service,
	certificateService certificate.Service,
	locations LocationStore,
	eventBus events.EventBus,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		heartbeatService:   heartbeatService,
		certificateService: certificateService,
		locations:          locations,
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

// monitorStatus is the status of a monitor from its locations: Down while any online location is
// Down, otherwise Maintenance while any is in maintenance, otherwise Up while any is Up, otherwise
// Pending. A location with no status yet, and one whose probe is offline, is unknown and counts for
// nothing: an offline probe is not Down, and a monitor none of whose locations is known is Pending.
func monitorStatus(locations []shared.MonitorLocation) shared.MonitorStatus {
	var anyUp, anyMaintenance bool
	for _, l := range locations {
		if !l.Online || l.Status == nil {
			continue
		}
		switch *l.Status {
		case shared.MonitorStatusDown:
			return shared.MonitorStatusDown
		case shared.MonitorStatusUp:
			anyUp = true
		case shared.MonitorStatusMaintenance:
			anyMaintenance = true
		}
	}
	switch {
	case anyMaintenance:
		return shared.MonitorStatusMaintenance
	case anyUp:
		return shared.MonitorStatusUp
	default:
		return shared.MonitorStatusPending
	}
}

// anyStatusKnown reports whether any location, online or not, has a status
func anyStatusKnown(locations []shared.MonitorLocation) bool {
	for _, l := range locations {
		if l.Status != nil {
			return true
		}
	}
	return false
}

// withLocation returns the locations with the probe's among them, changed by update. The probe that
// sent the result is online for as long as it takes to judge it, whatever its status says, and a
// monitor that has no row for it (MongoDB's, or one moved off the probe while its result was on
// the way) is judged on it all the same.
func withLocation(locations []shared.MonitorLocation, probeID string, update func(*shared.MonitorLocation)) []shared.MonitorLocation {
	judged := slices.Clone(locations)
	i := slices.IndexFunc(judged, func(l shared.MonitorLocation) bool { return l.ProbeID == probeID })
	if i < 0 {
		judged = append(judged, shared.MonitorLocation{ProbeID: probeID})
		i = len(judged) - 1
	}
	judged[i].Online = true
	update(&judged[i])
	return judged
}

// countsResends reports whether the probe's Down heartbeats count toward the resend interval:
// they do unless another online location that is Down has an id that sorts first, so that two Down
// locations don't resend twice as often as one
func countsResends(locations []shared.MonitorLocation, probeID string) bool {
	for _, l := range locations {
		if l.ProbeID < probeID && l.Online && l.Status != nil && *l.Status == shared.MonitorStatusDown {
			return false
		}
	}
	return true
}

// Ingest stores the result as a heartbeat and publishes the events that follow from it.
//
// The heartbeat is judged against the previous one of the same monitor and probe: its retries, and
// whether it is Pending or Down. That is the status of the monitor's location on the probe. The
// monitor's own status follows from its locations (see monitorStatus), and whether the heartbeat
// is important, whether to notify and whether the monitor changed status follow from the monitor's
// status before the result and after it. A monitor on one probe is judged exactly as it was when a
// monitor had no locations.
func (s *ServiceImpl) Ingest(ctx context.Context, r *Result) error {
	probeID := r.ProbeID
	if probeID == "" {
		probeID = shared.LocalProbeID
	}

	// Get the previous heartbeat of the monitor on this probe
	previousBeats, err := s.heartbeatService.FindByMonitorIDAndProbeIDPaginated(ctx, r.MonitorID, probeID, 1, 0, nil, false)
	var previousBeat *heartbeat.Model = nil
	if err != nil {
		s.logger.Errorw("Failed to get previous heartbeat for monitor",
			"monitor_id", r.MonitorID,
			"probe_id", probeID,
			"error", err,
		)
	}
	if len(previousBeats) > 0 {
		previousBeat = previousBeats[0]
	}

	// The probe's first heartbeat of the monitor
	isFirstOnProbe := previousBeat == nil

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

	if !isFirstOnProbe {
		hb.DownCount = previousBeat.DownCount
		hb.Retries = previousBeat.Retries
	}

	// Mark as pending if max retries is set and retries is less than max retries
	if r.Status == shared.MonitorStatusDown {
		if !isFirstOnProbe && r.MonitorMaxRetries > 0 && previousBeat.Retries < r.MonitorMaxRetries {
			hb.Status = shared.MonitorStatusPending
		}
		hb.Retries++
	} else {
		hb.Retries = 0
	}
	if hb.Status == shared.MonitorStatusUp {
		// No Down heartbeat is counted toward a resend while the location is Up
		hb.DownCount = 0
	}

	// Record the location's status, and read the monitor's locations from before and after
	locationStatus := hb.Status
	state := shared.LocationState{ProbeID: probeID, Status: &locationStatus, LastPing: &r.PingMs, LastHeartbeatAt: &r.StartTime}
	recordedBefore, recordedAfter, err := s.locations.RecordLocation(ctx, r.MonitorID, state)
	if err != nil {
		s.logger.Errorw("Failed to record the location of monitor", "monitor_id", r.MonitorID, "probe_id", probeID, "error", err)
		return fmt.Errorf("failed to record the location: %w", err)
	}
	// A location with no status yet but a heartbeat on the probe has that heartbeat's status: the
	// row of a monitor from before it kept one, and MongoDB's has none
	locationsBefore := withLocation(recordedBefore, probeID, func(l *shared.MonitorLocation) {
		if l.Status == nil && previousBeat != nil {
			previousStatus := previousBeat.Status
			l.Status = &previousStatus
		}
	})
	locationsAfter := withLocation(recordedAfter, probeID, func(l *shared.MonitorLocation) {
		l.Status, l.LastPing, l.LastHeartbeatAt = &locationStatus, &r.PingMs, &r.StartTime
	})

	// The monitor's status before this result and after it. The monitor's first heartbeat is the
	// first any of its locations has made.
	isFirstBeat := !anyStatusKnown(locationsBefore)
	previousStatus := monitorStatus(locationsBefore)
	currentStatus := monitorStatus(locationsAfter)

	isImportant := isFirstBeat || s.isImportantBeat(previousStatus, currentStatus)
	shouldNotify := false

	// If important (the monitor's status changed), send notification
	if isImportant {
		hb.Important = true

		if isFirstBeat || s.isImportantForNotification(previousStatus, currentStatus) {
			s.logger.Debugw("Marking for notification", "monitor_name", r.MonitorName)
			shouldNotify = true
			hb.Notified = true
		}

		hb.DownCount = 0
	} else {
		hb.Important = false

		if r.Status == shared.MonitorStatusDown && r.MonitorResendInt > 0 && countsResends(locationsAfter, probeID) {
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
		s.restoreLocation(ctx, r.MonitorID, probeID, recordedBefore)
		return fmt.Errorf("failed to create heartbeat: %w", err)
	}

	// Publish events. The status the monitor changed to is the monitor's, which is not always the
	// heartbeat's: the event sets the monitor's status.
	if isFirstBeat || previousStatus != currentStatus {
		statusChange := *dbHb
		statusChange.Status = currentStatus
		if err := events.Publish(ctx, s.eventBus, events.MonitorStatusChanged, statusChange); err != nil {
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

// restoreLocation puts the probe's row back as it was before the result was recorded on it, for a
// result that could not be stored: the task is tried again, and judged against the monitor as it
// was, so the notification it is due is not lost. It cannot do more than log a failure, and a row
// the monitor has not got is not made.
func (s *ServiceImpl) restoreLocation(ctx context.Context, monitorID, probeID string, before []shared.MonitorLocation) {
	i := slices.IndexFunc(before, func(l shared.MonitorLocation) bool { return l.ProbeID == probeID })
	if i < 0 {
		return
	}
	// The failure may be the context's own
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	old := before[i]
	_, _, err := s.locations.RecordLocation(ctx, monitorID, shared.LocationState{ProbeID: probeID, Status: old.Status, LastPing: old.LastPing, LastHeartbeatAt: old.LastHeartbeatAt})
	if err != nil {
		s.logger.Errorw("Failed to put the location of monitor back after a failed heartbeat", "monitor_id", monitorID, "probe_id", probeID, "error", err)
	}
}
