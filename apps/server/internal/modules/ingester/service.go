package ingester

import (
	"context"
	"fmt"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester/ingest"
	"peekaping/internal/modules/shared"
	"slices"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Result and Service are package ingest's, where the probe's scheduler reaches them without the
// storage behind this package. They are aliased here for the rest of the server.
type (
	Result  = ingest.Result
	Service = ingest.Service
)

// LocationStore keeps a monitor's state on each probe it runs on, which the ingester reads and
// writes to judge the monitor as a whole. The monitor repository is one.
type LocationStore interface {
	// RecordLocation stores what the result left on the monitor's row for the probe, calls judge
	// with the monitor's last recorded status and its rows from just before and just after, and
	// records the status judge returns (nil clears it). All of it happens with the monitor held,
	// so two results of one monitor, recorded at once, see one another in turn. A monitor without
	// a row for the probe has nothing stored on a row, and a monitor without rows (one from before
	// probes) is judged on none.
	RecordLocation(ctx context.Context, monitorID string, state shared.LocationState, judge func(shared.LocationJudgement) *shared.MonitorStatus) error
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

// locationRule is a monitor's location rule as the ingester judges by it: how many of the
// locations that count must be Down for the monitor to be Down
type locationRule struct {
	rule shared.LocationRule
	// threshold is how many Down locations at_least asks for
	threshold int
}

// ruleOf is the location rule a result carries. An empty or unknown rule is any.
func ruleOf(r *Result) locationRule {
	switch r.MonitorLocationRule {
	case shared.LocationRuleAll:
		return locationRule{rule: shared.LocationRuleAll}
	case shared.LocationRuleAtLeast:
		return locationRule{rule: shared.LocationRuleAtLeast, threshold: r.MonitorLocationThreshold}
	default:
		return locationRule{rule: shared.LocationRuleAny}
	}
}

// met reports whether the Down locations among those that count are enough to take the monitor
// Down. At least needs a threshold that is met outright, so that a threshold of zero, which no
// monitor is given, never takes a monitor down on no evidence.
func (l locationRule) met(down, counted int) bool {
	switch l.rule {
	case shared.LocationRuleAll:
		return counted > 0 && down == counted
	case shared.LocationRuleAtLeast:
		return l.threshold > 0 && down >= l.threshold
	default:
		return down > 0
	}
}

// monitorStatus is the status of a monitor from its locations: Down while the location rule is met
// by the online locations that have a status, otherwise Maintenance while any of them is in
// maintenance, otherwise Up while any is Up, otherwise Pending. A location with no status yet, and
// one whose probe is offline, is unknown and counts for nothing, neither as Down nor in the number
// "all" and "at least" are measured against: an offline probe is not Down, a monitor needing more
// Down locations than can report is never Down, and a monitor none of whose locations is known is
// Pending.
func monitorStatus(locations []shared.MonitorLocation, rule locationRule) shared.MonitorStatus {
	var counted, down int
	var anyUp, anyMaintenance bool
	for _, l := range locations {
		if !l.Online || l.Status == nil {
			continue
		}
		counted++
		switch *l.Status {
		case shared.MonitorStatusDown:
			down++
		case shared.MonitorStatusUp:
			anyUp = true
		case shared.MonitorStatusMaintenance:
			anyMaintenance = true
		}
	}
	switch {
	case rule.met(down, counted):
		return shared.MonitorStatusDown
	case anyMaintenance:
		return shared.MonitorStatusMaintenance
	case anyUp:
		return shared.MonitorStatusUp
	default:
		return shared.MonitorStatusPending
	}
}

// carriesChange reports whether a result whose location has the status can be the one that changes
// the monitor from one status to another. The heartbeat of the result is the one saved as
// important and notified, so a result can move the monitor to Up only if its own location is not
// Down, and to Down only if it is not Up: another location's stale status, back with its probe,
// must wait for that location's own result, which carries the change. A Pending or Maintenance
// location is neither, so it can move the monitor either way. No result moves a Down monitor to
// Pending: a Down monitor leaves Down only for Up or Maintenance, so that another location's
// retries, or a Docker container that is starting, don't hide the outage, and the Down to Up that
// ends it is the one notified.
func carriesChange(location, from, to shared.MonitorStatus) bool {
	if from == to {
		return true
	}
	switch to {
	case shared.MonitorStatusUp:
		return location != shared.MonitorStatusDown
	case shared.MonitorStatusDown:
		return location != shared.MonitorStatusUp
	case shared.MonitorStatusPending:
		return from != shared.MonitorStatusDown
	default:
		return true
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
// monitor that has no row for it (one moved off the probe while its result was on the way) is
// judged on it all the same.
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

// countsTowardResend reports whether a heartbeat stored with the status counts toward the resend
// interval while the monitor has the monitorStatus: only a Down heartbeat of a Down monitor does.
// A failed check stored as Pending is a retry, and a location can be Down while the monitor is not
// (under "all" or "at least" it can be Pending, Up or in maintenance): a Down notification then
// would say the monitor is down when it isn't.
func countsTowardResend(stored, monitorStatus shared.MonitorStatus) bool {
	return stored == shared.MonitorStatusDown && monitorStatus == shared.MonitorStatusDown
}

// Ingest stores the result as a heartbeat and publishes the events that follow from it.
//
// The heartbeat is judged against the previous one of the same monitor and probe: its retries, and
// whether it is Pending or Down. That is the status of the monitor's location on the probe. A
// location that was Down stays Down until a result is Up: a failed check, whether the check
// result is Down or Pending, is stored as Down and counted, whatever max retries has become. The
// monitor's own status follows from its locations (see monitorStatus), and whether the heartbeat
// is important, whether to notify and whether the monitor changed status follow from the monitor's
// status before the result and after it. A monitor on one probe is judged exactly as it was when a
// monitor had no locations. A result changes the monitor's status only when it can carry the change
// (see carriesChange); otherwise the monitor keeps the status last recorded for it.
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

	// A location whose previous heartbeat is Down stays Down until a result is Up
	downBefore := !isFirstOnProbe && previousBeat.Status == shared.MonitorStatusDown

	switch {
	case downBefore && (r.Status == shared.MonitorStatusDown || r.Status == shared.MonitorStatusPending):
		// A failed check keeps it Down, whatever max retries is now: raised since it went Down, it
		// does not send the location back to Pending. A check that reports Pending, such as a
		// Docker container that is still starting, is no recovery either: it is a failed check.
		hb.Status = shared.MonitorStatusDown
		hb.Retries++
	case r.Status == shared.MonitorStatusDown:
		// Mark as pending if max retries is set and retries is less than max retries
		if !isFirstOnProbe && r.MonitorMaxRetries > 0 && previousBeat.Retries < r.MonitorMaxRetries {
			hb.Status = shared.MonitorStatusPending
		}
		hb.Retries++
	default:
		hb.Retries = 0
	}
	if hb.Status == shared.MonitorStatusUp {
		// No Down heartbeat is counted toward a resend while the location is Up
		hb.DownCount = 0
	}

	rule := ruleOf(r)

	// Record the location's status, and judge the monitor while it is held: its status before this
	// result is the one last recorded for it, and its status after is what its locations add up to
	locationStatus := hb.Status
	state := shared.LocationState{ProbeID: probeID, Status: &locationStatus, LastPing: &r.PingMs, LastHeartbeatAt: &r.StartTime}
	var (
		rowsBefore      []shared.MonitorLocation
		recordedBefore  *shared.MonitorStatus
		isFirstBeat     bool
		previousStatus  shared.MonitorStatus
		currentStatus   shared.MonitorStatus
		locationsAfter  []shared.MonitorLocation
		locationsBefore []shared.MonitorLocation
	)
	err = s.locations.RecordLocation(ctx, r.MonitorID, state, func(j shared.LocationJudgement) *shared.MonitorStatus {
		rowsBefore, recordedBefore = j.Before, j.Recorded
		// A location with no status yet but a heartbeat on the probe has that heartbeat's status: the
		// row of a monitor from before it kept one
		locationsBefore = withLocation(j.Before, probeID, func(l *shared.MonitorLocation) {
			if l.Status == nil && previousBeat != nil {
				previousStatus := previousBeat.Status
				l.Status = &previousStatus
			}
		})
		locationsAfter = withLocation(j.After, probeID, func(l *shared.MonitorLocation) {
			l.Status, l.LastPing, l.LastHeartbeatAt = &locationStatus, &r.PingMs, &r.StartTime
		})
		// A monitor with a status recorded is first judged by it, whatever its locations say now:
		// a probe that went offline since then must not hide a change. One with none recorded goes by
		// its locations, and is on its first heartbeat when none has a status.
		isFirstBeat = j.Recorded == nil && !anyStatusKnown(locationsBefore)
		if j.Recorded != nil {
			previousStatus = *j.Recorded
		} else {
			previousStatus = monitorStatus(locationsBefore, rule)
		}
		currentStatus = monitorStatus(locationsAfter, rule)
		if !isFirstBeat && !carriesChange(locationStatus, previousStatus, currentStatus) {
			currentStatus = previousStatus
		}
		return &currentStatus
	})
	if err != nil {
		s.logger.Errorw("Failed to record the location of monitor", "monitor_id", r.MonitorID, "probe_id", probeID, "error", err)
		return fmt.Errorf("failed to record the location: %w", err)
	}

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

		if countsTowardResend(hb.Status, currentStatus) && r.MonitorResendInt > 0 && countsResends(locationsAfter, probeID) {
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
		s.restoreLocation(ctx, r.MonitorID, probeID, rowsBefore, recordedBefore)
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

// restoreLocation puts the probe's row, and the monitor's recorded status, back as they were before
// the result was recorded, for a result that could not be stored: the task is tried again, and
// judged against the monitor as it was, so the notification it is due is not lost. It cannot do
// more than log a failure, and a row the monitor has not got is not made.
func (s *ServiceImpl) restoreLocation(ctx context.Context, monitorID, probeID string, before []shared.MonitorLocation, recorded *shared.MonitorStatus) {
	var old shared.LocationState
	if i := slices.IndexFunc(before, func(l shared.MonitorLocation) bool { return l.ProbeID == probeID }); i >= 0 {
		old = shared.LocationState{ProbeID: probeID, Status: before[i].Status, LastPing: before[i].LastPing, LastHeartbeatAt: before[i].LastHeartbeatAt}
	} else {
		// No row to put back: a state for a probe the monitor has no row for stores nothing
		old = shared.LocationState{ProbeID: probeID}
	}
	// The failure may be the context's own
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	err := s.locations.RecordLocation(ctx, monitorID, old, func(shared.LocationJudgement) *shared.MonitorStatus { return recorded })
	if err != nil {
		s.logger.Errorw("Failed to put the location of monitor back after a failed heartbeat", "monitor_id", monitorID, "probe_id", probeID, "error", err)
	}
}
