package protocol

import (
	"context"

	"peekaping/internal/modules/monitor"
)

// MonitorSource lists the monitors assigned to a probe.
//
// It is the seam the handlers ask which monitors are a probe's, and know nothing about how they
// are assigned. What it returns is every monitor of the probe that is active, push monitors and
// types the probe can't run included. Dealable picks the ones to deal.
type MonitorSource interface {
	FindActiveByProbe(ctx context.Context, probeID string) ([]*monitor.Model, error)
}

// sourcePageSize is how many monitors the source reads from the monitor service at a time
const sourcePageSize = 200

// monitorSource is the MonitorSource over the monitors' own probes
type monitorSource struct {
	monitors monitor.Service
}

// NewMonitorSource returns the MonitorSource that reads the probe of each monitor from the
// monitor service
func NewMonitorSource(monitorService monitor.Service) MonitorSource {
	return &monitorSource{monitors: monitorService}
}

// FindActiveByProbe pages through the probe's monitors. A monitor created or deleted while it reads
// can move one across a page edge, so a monitor read twice is listed once.
func (s *monitorSource) FindActiveByProbe(ctx context.Context, probeID string) ([]*monitor.Model, error) {
	found := []*monitor.Model{}
	seen := map[string]bool{}
	for page := 0; ; page++ {
		monitors, err := s.monitors.FindActiveByProbe(ctx, probeID, page, sourcePageSize)
		if err != nil {
			return nil, err
		}
		for _, m := range monitors {
			if !seen[m.ID] {
				seen[m.ID] = true
				found = append(found, m)
			}
		}
		if len(monitors) < sourcePageSize {
			return found, nil
		}
	}
}
