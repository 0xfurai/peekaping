package protocol

import (
	"context"

	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"
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
	return func(ctx context.Context, m *shared.Monitor) (bool, error) {
		if m.RunsOn(shared.LocalProbeID) {
			return false, nil
		}
		for _, probeID := range m.ProbeIDs {
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
