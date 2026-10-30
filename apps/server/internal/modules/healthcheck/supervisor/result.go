package supervisor

import (
	"time"

	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/ingester/ingest"
	"peekaping/internal/modules/shared"
)

// TickResult is what HandleMonitorTick returns
type TickResult struct {
	// Raw execution result
	ExecutionResult *executor.Result

	// Monitor that was checked
	Monitor *shared.Monitor

	// Calculated ping time in milliseconds
	PingMs int

	// Whether the monitor is under maintenance
	IsUnderMaintenance bool
}

// IngestResult is the result as the ingest logic takes it, from the local probe, with the settings of the monitor
// that was checked. checkCertExpiry is whether the monitor's config asks for its TLS
// certificate's expiry to be checked. Single mode's scheduler builds what it ingests here.
func (r *TickResult) IngestResult(checkCertExpiry bool) *ingest.Result {
	m := r.Monitor
	start, end := utcTimes(r.ExecutionResult.StartTime, r.ExecutionResult.EndTime)
	return &ingest.Result{
		ProbeID:           shared.LocalProbeID,
		MonitorID:         m.ID,
		WorkspaceID:       m.WorkspaceID,
		MonitorName:       m.Name,
		MonitorType:       m.Type,
		MonitorInterval:   m.Interval,
		MonitorTimeout:    m.Timeout,
		MonitorMaxRetries: m.MaxRetries,
		MonitorRetryInt:   m.RetryInterval,
		MonitorResendInt:  m.ResendInterval,

		MonitorLocationRule:      m.LocationRule,
		MonitorLocationThreshold: m.LocationThresholdValue(),
		MonitorConfig:            m.Config,
		Status:                   r.ExecutionResult.Status,
		Message:                  r.ExecutionResult.Message,
		PingMs:                   r.PingMs,
		StartTime:                start,
		EndTime:                  end,
		IsUnderMaintenance:       r.IsUnderMaintenance,
		TLSInfo:                  r.ExecutionResult.TLSInfo,
		CheckCertExpiry:          checkCertExpiry,
	}
}

// utcTimes is the start and end of a check, in UTC, for ingest and storage. The executors keep the
// monotonic reading of their times, so end.Sub(start) is the time that passed even if the wall
// clock stepped during the check. UTC drops that reading, so the end is the start plus that
// difference: it can't come out before the start.
func utcTimes(start, end time.Time) (time.Time, time.Time) {
	duration := end.Sub(start)
	start = start.UTC()
	return start, start.Add(duration)
}
