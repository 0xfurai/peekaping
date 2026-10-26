package supervisor

import (
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
	return &ingest.Result{
		ProbeID:           shared.LocalProbeID,
		MonitorID:         m.ID,
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
		StartTime:                r.ExecutionResult.StartTime,
		EndTime:                  r.ExecutionResult.EndTime,
		IsUnderMaintenance:       r.IsUnderMaintenance,
		TLSInfo:                  r.ExecutionResult.TLSInfo,
		CheckCertExpiry:          checkCertExpiry,
	}
}
