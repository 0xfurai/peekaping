// Package ingest is the contract between what runs a check and what stores its result: the Result
// of a check, the Service that takes it, and the wrapper that has a monitor's results taken one at
// a time, in order. It has no storage behind it, so the remote probe's scheduler uses it too.
// Package ingester has the Service that stores a result.
package ingest

import (
	"context"
	"time"

	"peekaping/internal/modules/shared"
)

// Result is the outcome of one check, or of one push to a push monitor, with the monitor
// settings the ingest logic reads.
type Result struct {
	// ProbeID is the probe that ran the check, stored on the heartbeat. A result that names none
	// is the local probe's.
	ProbeID           string
	MonitorID         string
	MonitorName       string
	MonitorType       string
	MonitorInterval   int
	MonitorTimeout    int
	MonitorMaxRetries int
	MonitorRetryInt   int
	MonitorResendInt  int
	// MonitorLocationRule and MonitorLocationThreshold are the monitor's location rule. No rule
	// means any.
	MonitorLocationRule      shared.LocationRule
	MonitorLocationThreshold int
	MonitorConfig            string
	Status                   shared.MonitorStatus
	Message                  string
	PingMs                   int
	StartTime                time.Time
	EndTime                  time.Time
	IsUnderMaintenance       bool
	TLSInfo                  *shared.TLSInfo
	CheckCertExpiry          bool
}

// Service turns check results into heartbeats: ADR 0002's "ingest a result". Package ingester's
// NewService stores them. Single mode's checks and pushes, and the cluster API's pushes and probe
// results, call NewInOrderService over NewService. A remote probe's checks go to a Service that
// sends the results to the server.
type Service interface {
	// Ingest stores the result as a heartbeat and publishes the events that follow from it. It
	// reads the previous heartbeat of the monitor on the probe that ran the check to decide
	// Pending and Down, and the state of the monitor's other probes to decide whether the monitor
	// changed status, notify and so on. The results of one probe of a monitor must be stored one
	// at a time, in order; those of different probes of one monitor are ordered by LocationStore.
	Ingest(ctx context.Context, r *Result) error
}
