package app

import (
	"context"
	"errors"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
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
	if errors.Is(err, probe.ErrUnsupportedStorage) {
		return nil, monitor.ErrNoProbes
	}
	if err != nil || found == nil {
		return nil, err
	}
	return &monitor.ProbeInfo{Name: found.Name, SupportedTypes: found.SupportedTypes}, nil
}
