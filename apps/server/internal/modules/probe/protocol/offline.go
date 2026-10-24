package protocol

import (
	"context"

	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"
)

// MonitorProbeOffline returns what the stale-monitor watchdog asks before it decides a monitor is
// stale: whether the probe the monitor belongs to is offline. Such a monitor isn't late, its probe
// is gone, and the probe's own offline notification says so.
//
// The watchdog hands over the monitor it holds, with its probes, so only a monitor of a remote
// probe costs a read, and that is of the probe. The local probe is never offline for this
// purpose: its checks run in the Peekaping process itself, whether or not any remote instance ever
// said hello, so a stuck local monitor is the watchdog's to find. A monitor whose probe doesn't
// exist isn't skipped either: nothing is checking it, and nothing says why.
func MonitorProbeOffline(probes probe.Service) stale.ProbeOfflineFunc {
	return func(ctx context.Context, m *shared.Monitor) (bool, error) {
		if m.RunsOn(shared.LocalProbeID) {
			return false, nil
		}
		found, err := probes.FindByID(ctx, m.ProbeIDs[0])
		if err != nil || found == nil {
			return false, err
		}
		return found.Status == probe.StatusOffline, nil
	}
}
