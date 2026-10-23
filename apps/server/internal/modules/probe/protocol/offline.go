package protocol

import (
	"context"

	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/stale"
)

// MonitorProbeOffline returns what the stale-monitor watchdog asks before it decides a monitor is
// stale: whether the probe the monitor belongs to is offline. Such a monitor isn't late, its probe
// is gone, and the probe's own offline notification says so.
//
// The source says which probe a monitor belongs to, so that when monitors can be assigned to probes
// (#34) only the source changes. The local probe is never offline for this purpose: its checks run
// in the Peekaping process itself, whether or not any remote instance ever said hello, so a stuck
// local monitor is the watchdog's to find. A monitor whose probe doesn't exist isn't skipped
// either: nothing is checking it, and nothing says why.
func MonitorProbeOffline(source MonitorSource, probes probe.Service) stale.ProbeOfflineFunc {
	return func(ctx context.Context, monitorID string) (bool, error) {
		probeID, err := source.ProbeOf(ctx, monitorID)
		if err != nil {
			return false, err
		}
		if probeID == probe.LocalID {
			return false, nil
		}
		found, err := probes.FindByID(ctx, probeID)
		if err != nil || found == nil {
			return false, err
		}
		return found.Status == probe.StatusOffline, nil
	}
}
