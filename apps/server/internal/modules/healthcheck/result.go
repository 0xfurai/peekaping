package healthcheck

import (
	"time"

	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"go.uber.org/zap"
)

// ResultFromMonitor is a result that something outside the worker reports for a monitor, as the
// ingest logic takes it: the push URL's pushes and a probe's results. It names the local probe,
// which a probe's results replace with the probe that sent them. It carries the monitor's
// settings as they stand, and what is reported: the status, the message, the response time, the
// times of the check and the TLS information.
//
// A monitor inside a maintenance window reports Maintenance, as a scheduled check does:
// underMaintenance replaces the status and the message, and the response time is dropped, so
// planned downtime shows as neither Down nor Up and raises no notification.
//
// CheckCertExpiry follows the monitor's config, as the check loader reads it for the worker. A
// config that can't be read asks for nothing and is logged.
func ResultFromMonitor(
	m *monitor.Model,
	underMaintenance bool,
	status shared.MonitorStatus,
	message string,
	pingMs int,
	start, end time.Time,
	tlsInfo *certificate.TLSInfo,
	logger *zap.SugaredLogger,
) *ingester.Result {
	if underMaintenance {
		status = shared.MonitorStatusMaintenance
		message = maintenanceMessage
		pingMs = 0
	}
	checkCertExpiry, err := check.CertExpiryRequested(m.Config)
	if err != nil {
		logger.Warnw("Failed to parse monitor config for certificate expiry check",
			"monitor_id", m.ID,
			"monitor_name", m.Name,
			"error", err)
	}
	return &ingester.Result{
		ProbeID:            shared.LocalProbeID,
		MonitorID:          m.ID,
		MonitorName:        m.Name,
		MonitorType:        m.Type,
		MonitorInterval:    m.Interval,
		MonitorTimeout:     m.Timeout,
		MonitorMaxRetries:  m.MaxRetries,
		MonitorRetryInt:    m.RetryInterval,
		MonitorResendInt:   m.ResendInterval,
		MonitorConfig:      m.Config,
		Status:             status,
		Message:            message,
		PingMs:             pingMs,
		StartTime:          start,
		EndTime:            end,
		IsUnderMaintenance: underMaintenance,
		TLSInfo:            tlsInfo,
		CheckCertExpiry:    checkCertExpiry,
	}
}

// TickResult is what HandleMonitorTick returns
type TickResult struct {
	// Raw execution result
	ExecutionResult *executor.Result

	// Monitor that was checked
	Monitor *Monitor

	// Calculated ping time in milliseconds
	PingMs int

	// Whether the monitor is under maintenance
	IsUnderMaintenance bool
}

// IngestResult is the result as the ingest logic takes it, from the local probe, with the settings of the monitor
// that was checked. checkCertExpiry is whether the monitor's config asks for its TLS
// certificate's expiry to be checked. Cluster mode's worker and single mode's scheduler both
// build what they ingest here.
func (r *TickResult) IngestResult(checkCertExpiry bool) *ingester.Result {
	m := r.Monitor
	return &ingester.Result{
		ProbeID:            shared.LocalProbeID,
		MonitorID:          m.ID,
		MonitorName:        m.Name,
		MonitorType:        m.Type,
		MonitorInterval:    m.Interval,
		MonitorTimeout:     m.Timeout,
		MonitorMaxRetries:  m.MaxRetries,
		MonitorRetryInt:    m.RetryInterval,
		MonitorResendInt:   m.ResendInterval,
		MonitorConfig:      m.Config,
		Status:             r.ExecutionResult.Status,
		Message:            r.ExecutionResult.Message,
		PingMs:             r.PingMs,
		StartTime:          r.ExecutionResult.StartTime,
		EndTime:            r.ExecutionResult.EndTime,
		IsUnderMaintenance: r.IsUnderMaintenance,
		TLSInfo:            r.ExecutionResult.TLSInfo,
		CheckCertExpiry:    checkCertExpiry,
	}
}
