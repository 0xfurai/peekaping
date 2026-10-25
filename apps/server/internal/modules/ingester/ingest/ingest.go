// Package ingest is the contract between what runs a check and what stores its result: the Result
// of a check, the Service that takes it, and the wrapper that has a monitor's results taken one at
// a time, in order. It has no storage behind it, so the remote probe's scheduler uses it too.
// Package ingester has the Services that store a result or queue it for storing.
package ingest

import (
	"context"
	"time"

	"peekaping/internal/modules/shared"
)

// Result is the outcome of one check, or of one push to a push monitor, with the monitor
// settings the ingest logic reads. It is also the JSON payload of the ingester task: the
// worker, the API's push URL and the probe protocol's results route encode it, and the ingester
// decodes it.
type Result struct {
	// ProbeID is the probe that ran the check, stored on the heartbeat. The local worker and the
	// push URL set it to the local probe; a task queued before they did leaves it empty, which
	// the ingester also takes as the local probe.
	ProbeID           string `json:"probe_id,omitempty"`
	MonitorID         string `json:"monitor_id"`
	MonitorName       string `json:"monitor_name"`
	MonitorType       string `json:"monitor_type"`
	MonitorInterval   int    `json:"monitor_interval"`
	MonitorTimeout    int    `json:"monitor_timeout"`
	MonitorMaxRetries int    `json:"monitor_max_retries"`
	MonitorRetryInt   int    `json:"monitor_retry_interval"`
	MonitorResendInt  int    `json:"monitor_resend_interval"`
	// MonitorLocationRule and MonitorLocationThreshold are the monitor's location rule. A result
	// from before they existed has none, which means any.
	MonitorLocationRule      shared.LocationRule  `json:"monitor_location_rule,omitempty"`
	MonitorLocationThreshold int                  `json:"monitor_location_threshold,omitempty"`
	MonitorConfig            string               `json:"monitor_config"`
	Status                   shared.MonitorStatus `json:"status"`
	Message                  string               `json:"message"`
	PingMs                   int                  `json:"ping_ms"`
	StartTime                time.Time            `json:"start_time"`
	EndTime                  time.Time            `json:"end_time"`
	IsUnderMaintenance       bool                 `json:"is_under_maintenance"`
	TLSInfo                  *shared.TLSInfo      `json:"tls_info,omitempty"`
	CheckCertExpiry          bool                 `json:"check_cert_expiry"`
}

// Service turns check results into heartbeats: ADR 0002's "ingest a result". Package ingester's
// NewService stores them. On MongoDB the worker and the API's push URL get its NewQueueService and
// NewPushQueueService, which enqueue the results for the ingester, and the ingester stores them
// with NewService. Single mode's checks and pushes, and the cluster API's pushes and probe results
// on SQLite and PostgreSQL, call NewInOrderService over NewService. A remote probe's checks go to
// a Service that sends the results to the server.
type Service interface {
	// Ingest stores the result as a heartbeat and publishes the events that follow from it, or
	// hands it to the ingester that does. Storing reads the previous heartbeat of the monitor on
	// the probe that ran the check to decide Pending and Down, and the state of the monitor's
	// other probes to decide whether the monitor changed status, notify and so on. The results of
	// one probe of a monitor must be stored one at a time, in order; those of different probes
	// of one monitor are ordered by LocationStore.
	Ingest(ctx context.Context, r *Result) error
}
