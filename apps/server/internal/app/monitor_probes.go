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

// FindDefaultProbes returns the default probes that are not disabled, the local probe first. A
// disabled probe is skipped: a monitor assigned to it would not run.
func (l *probeLookup) FindDefaultProbes(ctx context.Context) ([]*monitor.DefaultProbe, error) {
	all, err := l.probes.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	var defaults []*monitor.DefaultProbe
	for _, p := range all {
		if p.IsDefault && !p.Disabled {
			defaults = append(defaults, &monitor.DefaultProbe{ID: p.ID, ProbeInfo: monitor.ProbeInfo{Name: p.Name, SupportedTypes: p.SupportedTypes}})
		}
	}
	return defaults, nil
}

// probeMonitorAssigner lets the probe controller assign a probe to monitors through the monitor
// service. The probe module doesn't import the monitor module, so this is where the two meet.
type probeMonitorAssigner struct {
	monitors monitor.Service
	probes   probe.Service
}

func provideProbeMonitorAssigner(monitors monitor.Service, probes probe.Service) probe.MonitorAssigner {
	return &probeMonitorAssigner{monitors: monitors, probes: probes}
}

// AssignProbeToAll always assigns: the action is the admin's own request, and not a flag that a
// hello spends once. It still makes the claim, so that a flag that is set (an admin made a probe
// with instances wait again, or its first hello hasn't come) is cleared in the same transaction as
// the assignment, and the probe page stops saying it waits for its first instance. The claim says
// true whether or not it found the flag.
func (a *probeMonitorAssigner) AssignProbeToAll(ctx context.Context, probeID string, supportedTypes []string) (int, error) {
	return a.monitors.AssignProbeToAllAnyWorkspace(ctx, probeID, supportedTypes, func(ctx context.Context) (bool, error) {
		if _, err := a.probes.ClaimPendingAssignment(ctx, probeID); err != nil {
			return false, err
		}
		return true, nil
	})
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
