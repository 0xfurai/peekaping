// Package check loads what a check needs to run: the monitor with its proxy, its
// maintenance state, and for a push monitor its latest heartbeat. Cluster mode's producer
// and single mode's scheduler both load their checks here.
package check

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/shared"
)

var (
	// ErrMonitorNotFound means the monitor no longer exists, for example because it was
	// deleted after its check was scheduled
	ErrMonitorNotFound = errors.New("monitor not found")
	// ErrMonitorPaused means the monitor is paused, so it gets no checks until it's resumed
	ErrMonitorPaused = errors.New("monitor is paused")
)

// Job is everything one check of a monitor needs, as the monitor stands when it is loaded
type Job struct {
	Monitor *shared.Monitor
	// Proxy is the monitor's proxy; nil when it has none or the proxy couldn't be loaded
	Proxy *shared.Proxy
	// IsUnderMaintenance is set while any of the monitor's maintenance windows is active
	IsUnderMaintenance bool
	// CheckCertExpiry is set when the monitor's config asks for its TLS certificate's expiry to be checked
	CheckCertExpiry bool
	// LastHeartbeat is the latest heartbeat of a push monitor; nil for other monitor types and
	// for a push monitor that has none yet
	LastHeartbeat *shared.HeartBeatModel
}

// MonitorFinder is the part of the monitor service the loader reads
type MonitorFinder interface {
	FindByID(ctx context.Context, id string) (*shared.Monitor, error)
}

// ProxyFinder is the part of the proxy service the loader reads
type ProxyFinder interface {
	FindByID(ctx context.Context, id string) (*shared.Proxy, error)
}

// MaintenanceReader is the part of the maintenance service the loader reads
type MaintenanceReader interface {
	GetMaintenancesByMonitorID(ctx context.Context, monitorID string) ([]*maintenance.Model, error)
	IsUnderMaintenance(ctx context.Context, m *maintenance.Model) (bool, error)
}

// HeartbeatFinder is the part of the heartbeat service the loader reads
type HeartbeatFinder interface {
	FindByMonitorIDPaginated(ctx context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*shared.HeartBeatModel, error)
}

// Loader loads the Job for a monitor's check
type Loader struct {
	monitorService     MonitorFinder
	proxyService       ProxyFinder
	maintenanceService MaintenanceReader
	heartbeatService   HeartbeatFinder
	logger             *zap.SugaredLogger
}

// NewLoader creates a loader
func NewLoader(
	monitorService MonitorFinder,
	proxyService ProxyFinder,
	maintenanceService MaintenanceReader,
	heartbeatService HeartbeatFinder,
	logger *zap.SugaredLogger,
) *Loader {
	return &Loader{
		monitorService:     monitorService,
		proxyService:       proxyService,
		maintenanceService: maintenanceService,
		heartbeatService:   heartbeatService,
		logger:             logger.With("component", "check_loader"),
	}
}

// Load reads what a check of the monitor needs. The job is nil whenever the error is not:
//   - ErrMonitorNotFound: there is no such monitor (anymore). Callers drop the check.
//   - ErrMonitorPaused: the monitor is paused. Callers drop the check.
//   - any other error: a read that must succeed failed (the monitor or its maintenance
//     windows). Callers may try again later.
//
// A proxy or a push monitor's latest heartbeat that can't be read doesn't fail the load.
// The check runs without it, and the failure is logged.
func (l *Loader) Load(ctx context.Context, monitorID string) (*Job, error) {
	mon, err := l.monitorService.FindByID(ctx, monitorID)
	if err != nil {
		return nil, fmt.Errorf("failed to find monitor: %w", err)
	}

	// The monitor might have been deleted
	if mon == nil {
		return nil, ErrMonitorNotFound
	}

	if !mon.Active {
		return nil, ErrMonitorPaused
	}

	isUnderMaintenance, err := l.isUnderMaintenance(ctx, monitorID)
	if err != nil {
		l.logger.Errorw("Failed to check if monitor is under maintenance", "monitor_id", monitorID, "error", err)
		return nil, err
	}

	job := &Job{
		Monitor:            mon,
		IsUnderMaintenance: isUnderMaintenance,
		CheckCertExpiry:    l.checkCertExpiry(mon),
	}

	// Fetch proxy if configured
	if mon.ProxyId != "" {
		proxyModel, err := l.proxyService.FindByID(ctx, mon.ProxyId)
		if err != nil {
			l.logger.Warnw("Failed to fetch proxy, continuing without it",
				"monitor_id", monitorID,
				"proxy_id", mon.ProxyId,
				"error", err)
		} else {
			job.Proxy = proxyModel
		}
	}

	// For push monitors, fetch the latest heartbeat
	if mon.Type == "push" {
		latestHeartbeats, err := l.heartbeatService.FindByMonitorIDPaginated(ctx, mon.ID, 1, 0, nil, false)
		if err != nil {
			l.logger.Warnw("Failed to fetch latest heartbeat for push monitor",
				"monitor_id", mon.ID,
				"error", err)
		} else if len(latestHeartbeats) > 0 {
			job.LastHeartbeat = latestHeartbeats[0]
		}
	}

	return job, nil
}

func (l *Loader) isUnderMaintenance(ctx context.Context, monitorID string) (bool, error) {
	maintenances, err := l.maintenanceService.GetMaintenancesByMonitorID(ctx, monitorID)
	if err != nil {
		return false, err
	}

	l.logger.Debugf("Found %d maintenances for monitor %s", len(maintenances), monitorID)

	for _, m := range maintenances {
		underMaintenance, err := l.maintenanceService.IsUnderMaintenance(ctx, m)
		if err != nil {
			l.logger.Warnf("Failed to get maintenance status for maintenance %s: %v", m.ID, err)
			continue
		}

		// If any maintenance is under-maintenance, the monitor is under maintenance
		if underMaintenance {
			return true, nil
		}
	}

	return false, nil
}

// checkCertExpiry reads whether the monitor's config asks for certificate expiry checking.
// It reads the flag for every type, so a monitor that supports TLS (http, smtp) only has
// to put check_cert_expiry in its config.
func (l *Loader) checkCertExpiry(mon *shared.Monitor) bool {
	checkCertExpiry, err := CertExpiryRequested(mon.Config)
	if err != nil {
		l.logger.Warnw("Failed to parse monitor config for certificate expiry check",
			"monitor_id", mon.ID,
			"monitor_name", mon.Name,
			"error", err)
	}
	l.logger.Debugw("Certificate expiry checking configured",
		"monitor_id", mon.ID,
		"monitor_name", mon.Name,
		"check_cert_expiry", checkCertExpiry)
	return checkCertExpiry
}

// CertExpiryRequested reports whether a monitor's config asks for the expiry of its TLS
// certificate to be checked: its check_cert_expiry flag, which every monitor type may carry. An
// empty config asks for nothing. A config that isn't JSON asks for nothing too, and the error
// says why.
func CertExpiryRequested(config string) (bool, error) {
	if config == "" {
		return false, nil
	}
	var flags struct {
		CheckCertExpiry bool `json:"check_cert_expiry"`
	}
	if err := json.Unmarshal([]byte(config), &flags); err != nil {
		return false, err
	}
	return flags.CheckCertExpiry, nil
}
