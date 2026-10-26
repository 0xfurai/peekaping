package protocol

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/probeproto"
	"peekaping/internal/utils"
)

// errProbeGone is returned when the probe a poll belongs to was deleted or disabled while it waited
var errProbeGone = errors.New("the probe is deleted or disabled")

// assignment is what one instance of a probe is told at one moment
type assignment struct {
	// version names the probe's assignment as a whole: it is the same for each of its instances
	version string
	// monitors are the instance's share of it
	monitors []probeproto.Monitor
}

// Assignments answers with the monitors that the calling instance runs. The instance sends the
// version it holds:
//   - a different version, or none, is answered at once with the instance's monitors and the
//     current version;
//   - the current version is held, until something changes (a monitor created, updated or deleted,
//     an instance of the probe joining or leaving) and the answer is the new assignment, or until
//     the poll times out, and the answer is 304 with no body.
//
// The instance's monitors are the probe's active monitors that are not push monitors, each dealt to
// one of the instances of the probe seen in the last minute that run its type: a probe whose
// instances are on different builds (the full image and the slim one) deals a monitor to the builds
// that have its executor. A monitor that none of them runs is dealt to nobody.
func (c *Controller) Assignments(ctx context.Context, input *AssignmentsInput) (*httpx.Output[probeproto.AssignmentsResponse], error) {
	// MARK: Assignments

	r := input.Request
	owner, instanceID, ok := ProbeFromContext(r.Context())
	if !ok {
		return nil, c.unauthenticated()
	}
	held := r.URL.Query().Get("version")

	// Every wait this poll makes ends with the poll
	pollCtx, endPoll := context.WithCancel(r.Context())
	defer endPoll()
	timeout := time.NewTimer(c.timing.PollTimeout)
	defer timeout.Stop()

	for {
		if r.Context().Err() != nil {
			// The probe hung up: nobody reads the answer
			return notModified()
		}
		// Wait before reading, not after: a change made between the two would end no wait
		wake := c.waker.Wait(pollCtx, owner.ID)

		current, err := c.assign(pollCtx, owner.ID, instanceID)
		switch {
		case errors.Is(err, errProbeGone):
			return nil, httpx.Fail(http.StatusUnauthorized, utils.NewFailResponse(messageInvalidToken))
		case err != nil && r.Context().Err() != nil:
			return notModified()
		case err != nil:
			return nil, c.fail(err)
		case current.version != held:
			return httpx.Reply(http.StatusOK, probeproto.AssignmentsResponse{Version: current.version, Monitors: current.monitors})
		}

		select {
		case <-wake:
			// Something changed, or the probe hung up. The version says which.
		case <-c.waker.Closed():
			return notModified()
		case <-timeout.C:
			return notModified()
		}
	}
}

// notModified is the answer to a poll that found nothing new: 304, and no body
func notModified() (*httpx.Output[probeproto.AssignmentsResponse], error) {
	return httpx.Reply(http.StatusNotModified, probeproto.AssignmentsResponse{})
}

// assign reads the database and works out what the instance is told. It is the same work for
// each of the probe's instances on each API replica: the version and the dealing depend on the
// database alone.
func (c *Controller) assign(ctx context.Context, probeID, instanceID string) (*assignment, error) {
	monitors, err := c.source.FindActiveByProbe(ctx, probeID)
	if err != nil {
		return nil, err
	}
	// The probe the middleware found has no instances, and its types may have changed since
	current, err := c.service.FindByID(ctx, probeID)
	if err != nil {
		return nil, err
	}
	if current == nil || current.Disabled {
		return nil, errProbeGone
	}

	// The instance that asks is online: the middleware has just stamped it. Its stamp and this
	// moment may fall either side of the window's edge though, and an instance that dealt itself
	// out would be dealt no monitors.
	online := probe.FilterOnline(current.Instances, c.now(), probe.InstanceOnlineWindow)
	if !slices.ContainsFunc(online, func(i *probe.Instance) bool { return i.ID == instanceID }) {
		if caller := findInstance(current.Instances, instanceID); caller != nil {
			online = append(online, caller)
		}
	}

	// A monitor is dealt among the instances that run its type, so the monitors to deal are those
	// that some online instance runs
	dealable := Dealable(monitors, probe.UnionTypes(online))
	proxies, err := c.proxiesOf(ctx, dealable)
	if err != nil {
		return nil, err
	}

	share := Deal(dealable, online, instanceID)
	slices.SortFunc(share, func(a, b *monitor.Model) int { return strings.Compare(a.ID, b.ID) })
	wire := make([]probeproto.Monitor, len(share))
	for i, m := range share {
		wire[i] = toWireMonitor(m, proxies)
	}
	return &assignment{version: Version(dealable, proxies, online), monitors: wire}, nil
}

// findInstance returns the instance with the id, or nil when there is none
func findInstance(instances []*probe.Instance, id string) *probe.Instance {
	for _, instance := range instances {
		if instance.ID == id {
			return instance
		}
	}
	return nil
}

// proxiesOf reads the proxies of the monitors, each once. A proxy that no longer exists is nil in
// the map: the monitor that names it has none.
func (c *Controller) proxiesOf(ctx context.Context, monitors []*monitor.Model) (map[string]*proxy.Model, error) {
	proxies := make(map[string]*proxy.Model)
	for _, m := range monitors {
		if m.ProxyId == "" {
			continue
		}
		if _, read := proxies[m.ProxyId]; read {
			continue
		}
		found, err := c.proxies.FindByID(ctx, m.ProxyId)
		if err != nil {
			return nil, err
		}
		proxies[m.ProxyId] = found
	}
	return proxies, nil
}

// toWireMonitor is a monitor as the probe is told it. The proxy comes with its password: a probe's
// token reads the secrets of the monitors it runs, as the probe needs them to check.
func toWireMonitor(m *monitor.Model, proxies map[string]*proxy.Model) probeproto.Monitor {
	wire := probeproto.Monitor{
		ID:             m.ID,
		Name:           m.Name,
		Type:           m.Type,
		Interval:       m.Interval,
		Timeout:        m.Timeout,
		MaxRetries:     m.MaxRetries,
		RetryInterval:  m.RetryInterval,
		ResendInterval: m.ResendInterval,
		Config:         m.Config,
		UpdatedAt:      m.UpdatedAt.UTC(),
	}
	if p := proxies[m.ProxyId]; m.ProxyId != "" && p != nil {
		wire.Proxy = &probeproto.Proxy{
			ID:        p.ID,
			Protocol:  p.Protocol,
			Host:      p.Host,
			Port:      p.Port,
			Auth:      p.Auth,
			Username:  p.Username,
			Password:  p.Password,
			UpdatedAt: p.UpdatedAt.UTC(),
		}
	}
	return wire
}
