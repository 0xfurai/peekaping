package protocol

import (
	"context"

	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"

	"go.uber.org/dig"
)

// MonitorProbeOffline returns what the stale-monitor watchdog asks before it decides a monitor is
// stale: whether every probe the monitor runs on is offline. Such a monitor isn't late, its probes
// are gone, and their own offline notifications say so.
//
// The watchdog hands over the monitor it holds, with its probes, so only a monitor of remote
// probes costs a read, and that is of the probes. The local probe is never offline for this
// purpose: its checks run in the Peekaping process itself, whether or not any remote instance ever
// said hello, so a stuck local monitor is the watchdog's to find, and so is a monitor with one
// probe that is online. A monitor whose probe doesn't exist isn't skipped either: nothing is
// checking it on that probe, and nothing says why.
func MonitorProbeOffline(probes probe.Service) stale.ProbeOfflineFunc {
	return MonitorProbeOfflineWith(probes, shared.LocalProbeIsTheProcess)
}

// MonitorProbeOfflineWith is MonitorProbeOffline for an install where the local probe may be its
// instances (shared.LocalProbeIsItsInstances): the cluster on SQLite and PostgreSQL, where no
// process of Peekaping's own runs checks. The local probe is then offline when its status says so,
// as a remote probe is, and a monitor that runs on it alone is left alone by the watchdog. A push
// monitor is the exception: it runs no checks, its pushes arrive at the api with or without an
// instance, so the watchdog still looks at it. The job that judges missing pushes can fail or
// stall, and the watchdog is what finds that out.
func MonitorProbeOfflineWith(probes probe.Service, local shared.LocalProbeLiveness) stale.ProbeOfflineFunc {
	return func(ctx context.Context, m *shared.Monitor) (bool, error) {
		if m.Type == pushType {
			return false, nil
		}
		if local == shared.LocalProbeIsTheProcess && m.RunsOn(shared.LocalProbeID) {
			return false, nil
		}
		probeIDs := m.ProbeIDs
		if len(probeIDs) == 0 {
			// A monitor read without its probes runs on the local one
			probeIDs = []string{shared.LocalProbeID}
		}
		for _, probeID := range probeIDs {
			found, err := probes.FindByID(ctx, probeID)
			if err != nil || found == nil {
				return false, err
			}
			if found.Status != probe.StatusOffline {
				return false, nil
			}
		}
		return true, nil
	}
}

// offlineParams are what the stale.ProbeOfflineFunc of an install is built from
type offlineParams struct {
	dig.In

	Probes probe.Service
	// LocalProbe is how the local probe is known to be up: the process itself when the install
	// provides none
	LocalProbe shared.LocalProbeLiveness `optional:"true"`
}

// provideMonitorProbeOffline provides the stale.ProbeOfflineFunc the watchdog asks
func provideMonitorProbeOffline(p offlineParams) stale.ProbeOfflineFunc {
	return MonitorProbeOfflineWith(p.Probes, p.LocalProbe)
}
