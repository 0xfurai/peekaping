package protocol

import (
	"context"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
)

// MonitorSource lists the monitors assigned to a probe. Until #34 adds monitor_probes, the local
// probe owns every active monitor and a remote probe owns none.
//
// It is the seam #34 replaces: the handlers ask it which monitors are a probe's, and know nothing
// about how they are assigned. What it returns is every monitor of the probe that is active, push
// monitors and types the probe can't run included. Dealable picks the ones to deal.
type MonitorSource interface {
	FindActiveByProbe(ctx context.Context, probeID string) ([]*monitor.Model, error)
	// ProbeOf returns the id of the probe the monitor is assigned to. The stale-monitor watchdog
	// asks it, to leave alone a monitor whose probe is offline.
	ProbeOf(ctx context.Context, monitorID string) (string, error)
}

// localOnlySource is the MonitorSource until monitors can be assigned to probes
type localOnlySource struct {
	monitors monitor.Service
}

// NewLocalOnlySource returns the MonitorSource of an install where monitors are not yet assigned to
// probes: every active monitor is the local probe's, and a remote probe has none.
func NewLocalOnlySource(monitorService monitor.Service) MonitorSource {
	return &localOnlySource{monitors: monitorService}
}

// ProbeOf is the local probe's, for every monitor
func (s *localOnlySource) ProbeOf(_ context.Context, _ string) (string, error) {
	return probe.LocalID, nil
}

func (s *localOnlySource) FindActiveByProbe(ctx context.Context, probeID string) ([]*monitor.Model, error) {
	if probeID != probe.LocalID {
		return []*monitor.Model{}, nil
	}
	return s.monitors.FindActive(ctx)
}
