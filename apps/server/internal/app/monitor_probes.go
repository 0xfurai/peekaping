package app

import (
	"context"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/stale"
)

// probeLookup answers the monitor service's questions about probes from the probe service. The
// monitor module doesn't import the probe module, so this is where the two meet.
type probeLookup struct {
	probes probe.Service
}

func provideProbeLookup(probes probe.Service) monitor.ProbeLookup {
	return &probeLookup{probes: probes}
}

func (l *probeLookup) FindProbe(ctx context.Context, id string) (*monitor.ProbeInfo, error) {
	found, err := l.probes.FindByID(ctx, id)
	if err != nil || found == nil {
		return nil, err
	}
	return &monitor.ProbeInfo{Name: found.Name, SupportedTypes: found.SupportedTypes}, nil
}

// staleProbeLookup answers the stale-monitor watchdog's questions about probes from the probe
// service. The stale module doesn't import the probe module, so this is where the two meet.
type staleProbeLookup struct {
	probes probe.Service
}

func provideStaleProbeLookup(probes probe.Service) stale.ProbeLookup {
	return &staleProbeLookup{probes: probes}
}

func (l *staleProbeLookup) FindProbe(ctx context.Context, id string) (*stale.ProbeInfo, error) {
	found, err := l.probes.FindByID(ctx, id)
	if err != nil || found == nil {
		return nil, err
	}
	return &stale.ProbeInfo{Name: found.Name, SupportedTypes: found.SupportedTypes}, nil
}
