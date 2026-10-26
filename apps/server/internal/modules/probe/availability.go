package probe

import (
	"context"
	"slices"
	"strings"
	"time"

	"peekaping/internal/modules/shared"

	"go.uber.org/dig"
)

// AvailabilityRange is how far back a probe's availability looks
type AvailabilityRange string

const (
	Range24h AvailabilityRange = "24h"
	Range7d  AvailabilityRange = "7d"
	Range30d AvailabilityRange = "30d"
)

// ConnectionsWindow is how far back the connections of a probe's instances go, whatever the range
const ConnectionsWindow = 24 * time.Hour

// ParseAvailabilityRange reads a range as the API takes it. An empty one is the default, 24h.
func ParseAvailabilityRange(s string) (AvailabilityRange, bool) {
	switch AvailabilityRange(s) {
	case "":
		return Range24h, true
	case Range24h, Range7d, Range30d:
		return AvailabilityRange(s), true
	}
	return "", false
}

// Duration is how long the range is
func (r AvailabilityRange) Duration() time.Duration {
	switch r {
	case Range7d:
		return 7 * 24 * time.Hour
	case Range30d:
		return 30 * 24 * time.Hour
	}
	return 24 * time.Hour
}

// Period is a stretch of time a probe was in one status
type Period struct {
	Status Status
	Start  time.Time
	End    time.Time
}

// Connection is a stretch of time a probe instance was connected
type Connection struct {
	Start time.Time
	End   time.Time
	// Open is whether the instance is connected now: it has not said bye and was seen within
	// InstanceOnlineWindow. Its End is now then.
	Open bool
}

// InstanceAvailability is a probe instance with its connections over the last 24 hours
type InstanceAvailability struct {
	ID          string
	Hostname    string
	Version     string
	Connections []Connection
}

// Availability is when a probe was online over a range
type Availability struct {
	ProbeID string
	Range   AvailabilityRange
	// From is the start of the range and To its end, now
	From time.Time
	To   time.Time
	// InProcess is true for the local probe in single mode: Peekaping's own process checks its
	// monitors, nothing connects to say so, and it has no periods, uptime or instances
	InProcess bool
	// TrackedFrom is the later of From and the probe's first recorded status. Nil when the probe
	// has no history.
	TrackedFrom *time.Time
	// UptimePercent is the online time over the time between TrackedFrom and To. Nil when that is
	// no time.
	UptimePercent *float64
	// Periods run one into the next from TrackedFrom to To, and never two in a row of one status
	Periods []Period
	// Instances are only on the availability of one probe
	Instances []InstanceAvailability
}

// AvailabilityService works out when probes were online, from the history the probe service keeps
type AvailabilityService interface {
	// Availability returns ErrNotFound when there is no such probe
	Availability(ctx context.Context, probeID string, rng AvailabilityRange) (*Availability, error)
	// AvailabilityOfAll returns every probe's availability, the local probe first, without the
	// instances
	AvailabilityOfAll(ctx context.Context, rng AvailabilityRange) ([]*Availability, error)
}

type availabilityService struct {
	probes   Service
	liveness shared.LocalProbeLiveness
	now      func() time.Time
}

// NewAvailabilityService makes the availability service. liveness says whether the local probe
// follows its status; now is the clock.
func NewAvailabilityService(probes Service, liveness shared.LocalProbeLiveness, now func() time.Time) AvailabilityService {
	return &availabilityService{probes: probes, liveness: liveness, now: now}
}

// availabilityParams are what the install's availability service is built from
type availabilityParams struct {
	dig.In

	Probes Service
	// LocalProbe is how the local probe is known to be up: the process itself when the install
	// provides none
	LocalProbe shared.LocalProbeLiveness `optional:"true"`
}

func provideAvailabilityService(p availabilityParams) AvailabilityService {
	return NewAvailabilityService(p.Probes, p.LocalProbe, time.Now)
}

// MARK: Availability
func (s *availabilityService) Availability(ctx context.Context, probeID string, rng AvailabilityRange) (*Availability, error) {
	found, err := s.probes.FindByID(ctx, probeID)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, ErrNotFound
	}
	now := s.now().UTC()
	result, err := s.periods(ctx, probeID, rng, now)
	if err != nil {
		return nil, err
	}
	if result.InProcess {
		return result, nil
	}
	result.Instances, err = s.instances(ctx, found, now)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// MARK: AvailabilityOfAll
func (s *availabilityService) AvailabilityOfAll(ctx context.Context, rng AvailabilityRange) ([]*Availability, error) {
	probes, err := s.probes.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	all := make([]*Availability, len(probes))
	for i, p := range probes {
		if all[i], err = s.periods(ctx, p.ID, rng, now); err != nil {
			return nil, err
		}
	}
	return all, nil
}

// periods works out the probe's periods and uptime over the range ending at now
func (s *availabilityService) periods(ctx context.Context, probeID string, rng AvailabilityRange, now time.Time) (*Availability, error) {
	from := now.Add(-rng.Duration())
	result := &Availability{ProbeID: probeID, Range: rng, From: from, To: now, Periods: []Period{}, Instances: []InstanceAvailability{}}
	if !s.liveness.FollowsItsStatus(probeID) {
		result.InProcess = true
		return result, nil
	}
	changes, err := s.probes.FindStatusChanges(ctx, probeID, from)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return result, nil
	}

	trackedFrom := changes[0].At.UTC()
	if trackedFrom.Before(from) {
		trackedFrom = from
	}
	if trackedFrom.After(now) {
		trackedFrom = now
	}
	result.TrackedFrom = &trackedFrom

	var online time.Duration
	for i, c := range changes {
		start, end := c.At.UTC(), now
		if i+1 < len(changes) {
			end = changes[i+1].At.UTC()
		}
		// A period that crosses the start or the end of what is covered is cut there
		if start.Before(trackedFrom) {
			start = trackedFrom
		}
		if end.After(now) {
			end = now
		}
		if !end.After(start) {
			continue
		}
		if last := len(result.Periods) - 1; last >= 0 && result.Periods[last].Status == c.Status {
			result.Periods[last].End = end
		} else {
			result.Periods = append(result.Periods, Period{Status: c.Status, Start: start, End: end})
		}
		if c.Status == StatusOnline {
			online += end.Sub(start)
		}
	}
	if covered := now.Sub(trackedFrom); covered > 0 {
		uptime := float64(online) / float64(covered) * 100
		result.UptimePercent = &uptime
	}
	return result, nil
}

// instances lists the probe's instances with their connections over the last ConnectionsWindow
func (s *availabilityService) instances(ctx context.Context, p *Model, now time.Time) ([]InstanceAvailability, error) {
	from := now.Add(-ConnectionsWindow)
	connections, err := s.probes.FindInstanceConnections(ctx, p.ID, from)
	if err != nil {
		return nil, err
	}
	byInstance := map[string][]Connection{}
	for _, c := range connections {
		span, ok := connectionSpan(c, from, now)
		if ok {
			byInstance[c.InstanceID] = append(byInstance[c.InstanceID], span)
		}
	}
	instances := make([]InstanceAvailability, 0, len(p.Instances))
	for _, instance := range p.Instances {
		spans := byInstance[instance.ID]
		if spans == nil {
			spans = []Connection{}
		}
		slices.SortFunc(spans, func(a, b Connection) int { return a.Start.Compare(b.Start) })
		instances = append(instances, InstanceAvailability{ID: instance.ID, Hostname: instance.Hostname, Version: instance.Version, Connections: spans})
	}
	// The instance that connected first comes first; those with no connection in the window last
	slices.SortStableFunc(instances, func(a, b InstanceAvailability) int {
		switch {
		case len(a.Connections) == 0 && len(b.Connections) == 0:
			return strings.Compare(a.ID, b.ID)
		case len(a.Connections) == 0:
			return 1
		case len(b.Connections) == 0:
			return -1
		}
		if c := a.Connections[0].Start.Compare(b.Connections[0].Start); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return instances, nil
}

// connectionSpan cuts a connection to [from, now]. It reports false for one that falls outside.
func connectionSpan(c *InstanceConnection, from, now time.Time) (Connection, bool) {
	start, end := c.StartedAt.UTC(), c.LastSeenAt.UTC()
	open := false
	switch {
	case c.EndedAt != nil:
		end = c.EndedAt.UTC()
	case now.Sub(end) <= InstanceOnlineWindow:
		// Not said bye and seen within the online window: still connected, up to now
		open, end = true, now
	}
	if start.Before(from) {
		start = from
	}
	if end.After(now) {
		end = now
	}
	if start.After(now) || end.Before(start) {
		return Connection{}, false
	}
	return Connection{Start: start, End: end, Open: open}, true
}
