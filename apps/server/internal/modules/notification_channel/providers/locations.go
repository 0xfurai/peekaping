package providers

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
)

// locationName is how a location is named in a notification: its probe's name, or the probe's id
// when the name is missing
func locationName(l shared.MonitorLocation) string {
	if l.ProbeName != "" {
		return l.ProbeName
	}
	return l.ProbeID
}

// inPageOrder returns the locations in the order the monitor page shows them: the local probe
// first, then the others by name. The monitor service lists them by probe id.
func inPageOrder(locations []shared.MonitorLocation) []shared.MonitorLocation {
	ordered := slices.Clone(locations)
	slices.SortStableFunc(ordered, func(a, b shared.MonitorLocation) int {
		if aLocal, bLocal := a.ProbeID == shared.LocalProbeID, b.ProbeID == shared.LocalProbeID; aLocal != bLocal {
			if aLocal {
				return -1
			}
			return 1
		}
		return cmp.Or(
			strings.Compare(strings.ToLower(locationName(a)), strings.ToLower(locationName(b))),
			strings.Compare(a.ProbeID, b.ProbeID),
		)
	})
	return ordered
}

// countsAs reports whether the location counts: its probe is online and it has a status, and the
// status is the given one
func countsAs(l shared.MonitorLocation, status shared.MonitorStatus) bool {
	return l.Online && l.Status != nil && *l.Status == status
}

// LocationsDown are the names of the monitor's locations that are Down, in the order the monitor
// page shows them. A location whose probe is offline is not Down: its status is unknown.
func LocationsDown(m *monitor.Model) []string {
	names := []string{}
	for _, l := range inPageOrder(m.Locations) {
		if countsAs(l, shared.MonitorStatusDown) {
			names = append(names, locationName(l))
		}
	}
	return names
}

// LocationsTotal is how many locations the monitor has: one for each probe it runs on. A monitor
// read without any runs on the local probe alone.
func LocationsTotal(m *monitor.Model) int {
	return max(len(m.Locations), 1)
}

// LocationSummary says where a monitor that runs on more than one probe is Down or Up, for the
// notification of a heartbeat with the status. For Down it names the locations that are Down:
// "Down at Frankfurt, Tokyo (2 of 3 locations)". For Up it counts the locations that are Up:
// "Up at all locations", or "Up at 2 of 3 locations" when some are still Down, offline or not
// reporting. It is empty for a monitor on one probe, whose notification needs no places, and for
// any other status.
func LocationSummary(m *monitor.Model, status shared.MonitorStatus) string {
	total := LocationsTotal(m)
	if total < 2 {
		return ""
	}
	switch status {
	case shared.MonitorStatusDown:
		down := LocationsDown(m)
		if len(down) == 0 {
			// The locations changed since the heartbeat: there is nothing to name
			return ""
		}
		return fmt.Sprintf("Down at %s (%d of %d locations)", strings.Join(down, ", "), len(down), total)
	case shared.MonitorStatusUp:
		up := 0
		for _, l := range m.Locations {
			if countsAs(l, shared.MonitorStatusUp) {
				up++
			}
		}
		if up == total {
			return "Up at all locations"
		}
		return fmt.Sprintf("Up at %d of %d locations", up, total)
	default:
		return ""
	}
}
