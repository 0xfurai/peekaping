package protocol

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/netguard"
	"peekaping/internal/probeproto"
	"peekaping/internal/utils"
	"peekaping/internal/version"

	"github.com/danielgtaylor/huma/v2"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// The most characters hello's fields may have, as many as the registry's columns hold
const (
	maxHostnameLength     = 255
	maxOSLength           = 64
	maxArchLength         = 64
	maxProbeVersionLength = 64
	maxMonitorTypeLength  = 64
	maxSupportedTypes     = 256
)

type Controller struct {
	service     probe.Service
	source      MonitorSource
	monitors    monitor.Service
	proxies     proxy.Service
	maintenance maintenance.Service
	ingester    ingester.Service
	waker       *Waker
	bus         events.EventBus
	timing      Timing
	guard       *netguard.Guard
	// checksPerHost is the server's CHECKS_PER_HOST_PER_MINUTE, which hello tells the probe
	checksPerHost int
	accepted      Window
	// storeSlots limits the monitors all the results requests store at the same time
	storeSlots chan struct{}
	// unguarded holds the ids of the instances already warned of, for the warning that an instance
	// whose hello doesn't enforce the private target block is dealt no monitors: once each
	unguarded sync.Map
	now       func() time.Time
	logger    *zap.SugaredLogger
}

type ControllerParams struct {
	dig.In
	Probes      probe.Service
	Source      MonitorSource
	Monitors    monitor.Service
	Proxies     proxy.Service
	Maintenance maintenance.Service
	// Ingester stores the results a probe sends, as it does the pushes: in the process, in order
	Ingester ingester.Service
	// Window remembers the result ids already taken: Redis in cluster mode, so that a retry on
	// another API replica is found, and the process's memory in single mode. A container that
	// provides none gets the memory window.
	Window Window `optional:"true"`
	Waker  *Waker
	Bus    events.EventBus
	Timing Timing
	// Guard says whether the server blocks private targets, which hello tells the probe. A
	// container that has none blocks nothing.
	Guard *netguard.Guard `optional:"true"`
	// Config gives the server's CHECKS_PER_HOST_PER_MINUTE, which hello tells the probe. A container
	// that has none states no cap.
	Config *config.Config `optional:"true"`
	Logger *zap.SugaredLogger
}

func NewController(p ControllerParams) *Controller {
	window := p.Window
	if window == nil {
		window = NewMemoryWindow()
	}
	timing := p.Timing
	if timing.StoreDeadline <= 0 {
		timing.StoreDeadline = StoreDeadline
	}
	checksPerHost := 0
	if p.Config != nil {
		checksPerHost = p.Config.ChecksPerHostPerMinute
	}
	return &Controller{
		checksPerHost: checksPerHost,
		service:       p.Probes,
		source:        p.Source,
		monitors:      p.Monitors,
		proxies:       p.Proxies,
		maintenance:   p.Maintenance,
		ingester:      p.Ingester,
		waker:         p.Waker,
		bus:           p.Bus,
		timing:        timing,
		guard:         p.Guard,
		accepted:      window,
		storeSlots:    make(chan struct{}, StoreConcurrencyTotal),
		now:           time.Now,
		logger:        p.Logger.Named("[probe-protocol-controller]"),
	}
}

// instancesChanged tells the API replicas that the instances a probe is dealt among have changed,
// so that they answer the probe's waiting polls. The change is stored already, so a failure to say
// so only delays the siblings until their poll times out.
func (c *Controller) instancesChanged(ctx context.Context, probeID string) {
	if err := events.Publish(ctx, c.bus, probe.InstancesChanged, probe.InstancesChangedEvent{ProbeID: probeID}); err != nil {
		c.logger.Warnw("Failed to publish that the instances of a probe changed", "probeId", probeID, "error", err)
	}
}

// fail turns a service error into the response that tells the probe what went wrong
func (c *Controller) fail(err error) error {
	switch {
	case errors.Is(err, probe.ErrInstanceTaken):
		return httpx.Fail(http.StatusConflict, utils.NewFailResponse("this instance id belongs to another probe: use a new one"))
	}
	c.logger.Errorw("Probe protocol request failed", "error", err)
	return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse(messageInternalError))
}

// tooManyInstances is the 429 for a hello from a new instance of a probe that has all
// probe.MaxInstancesPerProbe of its instances running. Retry-After is a window: by then an instance
// that has stopped is no longer online, and the hello can take its place. The probe client waits
// for it, as for any 429.
func tooManyInstances(owner *probe.Model) error {
	message := "probe " + owner.Name + " already has " + strconv.Itoa(probe.MaxInstancesPerProbe) + " running instances, the most one probe may have"
	retryAfter := strconv.Itoa(int(probe.InstanceOnlineWindow / time.Second))
	return huma.ErrorWithHeaders(
		httpx.Fail(http.StatusTooManyRequests, utils.NewFailResponse(message)),
		http.Header{"Retry-After": {retryAfter}},
	)
}

// unavailable answers 503 for a request that failed because something the server shares could not
// be reached, so that the probe sends it again: its client waits and says hello first
func (c *Controller) unavailable(what string, err error) error {
	c.logger.Errorw("Probe protocol request could not be served", "what", what, "error", err)
	return httpx.Fail(http.StatusServiceUnavailable, utils.NewFailResponse(messageUnavailable))
}

// unauthenticated is for a route that the bearer middleware does not guard, which is a mistake in
// the route's setup and not the probe's
func (c *Controller) unauthenticated() error {
	c.logger.Errorw("A probe route was reached without authenticating the probe")
	return httpx.Fail(http.StatusUnauthorized, utils.NewFailResponse(messageTokenRequired))
}

// Hello introduces an instance: it records the instance and the monitor types its probe can run,
// and answers with the server's version and time, the probe's name and location, and the limits
// the probe works within.
func (c *Controller) Hello(ctx context.Context, input *HelloInput) (*httpx.Output[probeproto.HelloResponse], error) {
	// MARK: Hello

	r := input.Request
	owner, instanceID, ok := ProbeFromContext(r.Context())
	if !ok {
		return nil, c.unauthenticated()
	}
	req, err := decodeHello(r, instanceID)
	if err != nil {
		return nil, err
	}

	now := c.now().UTC()
	if err := c.recordInstance(r.Context(), owner, instanceID, req, now); err != nil {
		return nil, err
	}
	if err := c.assignOnHello(r.Context(), owner, instanceID, req); err != nil {
		return nil, err
	}
	// The instance is on record, so the probe is connected. The middleware's stamp does this for
	// every other route.
	cameBack(r.Context(), c.service, c.bus, c.logger, owner, now)

	return httpx.Reply(http.StatusOK, probeproto.HelloResponse{
		ProtocolVersion: probeproto.ProtocolVersion,
		ServerVersion:   version.Version,
		ServerTime:      now,
		Probe:           probeproto.ProbeInfo{ID: owner.ID, Name: owner.Name, Location: owner.Location},
		PollTimeoutMs:   int(c.timing.PollTimeout / time.Millisecond),
		FlushIntervalMs: int(FlushInterval / time.Millisecond),
		BatchMax:        BatchMax,
		BufferMax:       BufferMax,

		BlockPrivateTargets:    c.guard.Enabled(),
		ChecksPerHostPerMinute: c.checksPerHost,
	})
}

// decodeHello reads the hello of the request and checks it: the 426 for a protocol version that is
// too old, or the 400 for a hello that is malformed. instanceID is the one the request's header
// names.
func decodeHello(r *http.Request, instanceID string) (*probeproto.HelloRequest, error) {
	var req probeproto.HelloRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}

	if req.ProtocolVersion < MinProtocolVersion {
		return nil, httpx.Fail(http.StatusUpgradeRequired, utils.NewFailResponse(
			"protocol version "+strconv.Itoa(req.ProtocolVersion)+" is below the server's minimum "+strconv.Itoa(MinProtocolVersion)+": upgrade the probe"))
	}
	if message := validateHello(&req, instanceID); message != "" {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(message))
	}
	return &req, nil
}

// recordInstance puts the hello's instance on record, and tells the siblings when that changes
// what they are dealt among
func (c *Controller) recordInstance(ctx context.Context, owner *probe.Model, instanceID string, req *probeproto.HelloRequest, now time.Time) error {
	// What the siblings were dealt among before this hello: the instance as it was, if it was
	before, err := c.service.FindInstance(ctx, owner.ID, instanceID)
	if err != nil {
		return c.fail(err)
	}

	err = c.service.RegisterInstance(ctx, owner.ID, &probe.Instance{
		ID:              instanceID,
		Hostname:        req.Hostname,
		OS:              req.OS,
		Arch:            req.Arch,
		Version:         req.ProbeVersion,
		ProtocolVersion: req.ProtocolVersion,
		SupportedTypes:  req.SupportedTypes,
		LastSeenAt:      now,

		EnforcesPrivateTargetBlock: req.EnforcesPrivateTargetBlock,
	})
	if errors.Is(err, probe.ErrTooManyInstances) {
		return tooManyInstances(owner)
	}
	if err != nil {
		return c.fail(err)
	}
	// The siblings hear of the instance before the assignment is tried: it is on record now, so a
	// hello sent again after a failed assignment finds nothing changed and wouldn't tell them.
	if changesAssignments(before, req.SupportedTypes, now) {
		c.instancesChanged(ctx, owner.ID)
	}
	return nil
}

// assignOnHello has the probe take over its monitors, if it was waiting to, by the first instance
// that is dealt monitors
func (c *Controller) assignOnHello(ctx context.Context, owner *probe.Model, instanceID string, req *probeproto.HelloRequest) error {
	// An instance that would ignore the server's block is dealt nothing (see dealtTo), so it doesn't
	// claim the assign-all flag either: the probe's next instance that can run its monitors does.
	if c.guard.Enabled() && !req.EnforcesPrivateTargetBlock {
		c.warnUnguarded(owner, instanceID, req.ProbeVersion)
		return nil
	}
	// The flag is read fresh, by the claim, now that the instance is on record, and not from the
	// token lookup that made owner: an assign-all action that set it after that lookup is still
	// seen. A probe that isn't waiting costs one conditional update here.
	return c.assignAllMonitors(ctx, owner.ID, req.SupportedTypes)
}

// warnUnguarded logs, once for each instance, that the instance is dealt no monitors because its
// hello didn't say it blocks private targets, which a probe from before BLOCK_PRIVATE_TARGETS
// doesn't
func (c *Controller) warnUnguarded(owner *probe.Model, instanceID, probeVersion string) {
	if _, warned := c.unguarded.LoadOrStore(instanceID, struct{}{}); warned {
		return
	}
	c.logger.Warnw("Probe instance gets no monitors: BLOCK_PRIVATE_TARGETS is on, and the probe is too old to block private targets. Upgrade the probe.",
		"probeId", owner.ID, "probeName", owner.Name, "instanceId", instanceID, "probeVersion", probeVersion)
}

// assignAllMonitors gives a probe that was made to take over every monitor the ones its first
// instance runs, and does nothing for a probe that isn't waiting: the claim finds no flag and the
// transaction ends before anything else is read or written. The claim that clears the probe's flag is made in the same transaction as the
// assignment, so a reconnect, a second instance or the other api replica that gets a hello of its
// own at the same time assigns nothing more. A failure answers 503 and leaves the flag set: the
// probe says hello again, and that hello assigns.
func (c *Controller) assignAllMonitors(ctx context.Context, probeID string, supportedTypes []string) error {
	assigned, err := c.monitors.AssignProbeToAllAnyWorkspace(ctx, probeID, supportedTypes, func(ctx context.Context) (bool, error) {
		return c.service.ClaimPendingAssignment(ctx, probeID)
	})
	if err != nil {
		return c.unavailable("assign the probe its monitors", err)
	}
	if assigned > 0 {
		c.logger.Infow("Probe assigned its monitors", "probeId", probeID, "monitors", assigned)
	}
	return nil
}

// Bye retires the instance that says it is stopping. It does not delete it: the instance is left
// last seen just outside the online window (probe.RetiredAt), so that it is out of the dealing at once,
// while the probe's offline threshold keeps counting from that time. A probe that restarts
// gracefully says hello again well within it, and so raises no notification.
func (c *Controller) Bye(ctx context.Context, input *ByeInput) (*httpx.Output[httpx.NoBody], error) {
	// MARK: Bye

	r := input.Request
	owner, instanceID, ok := ProbeFromContext(r.Context())
	if !ok {
		return nil, c.unauthenticated()
	}
	now := c.now().UTC()
	if err := c.service.RetireInstance(r.Context(), owner.ID, instanceID, probe.RetiredAt(now), now); err != nil {
		return nil, c.fail(err)
	}
	c.instancesChanged(r.Context(), owner.ID)

	return httpx.Reply(http.StatusNoContent, httpx.NoBody{})
}

// changesAssignments reports whether a hello with supportedTypes changes what the probe's instances
// are told: the instance was not online, so the others are dealt among one more, or the types it
// runs change, which moves the monitors of those types between it and the others. before is the
// instance as it was, nil if the probe had no such instance.
func changesAssignments(before *probe.Instance, supportedTypes []string, now time.Time) bool {
	if before == nil || len(probe.FilterOnline([]*probe.Instance{before}, now, probe.InstanceOnlineWindow)) == 0 {
		return true
	}
	return !slices.Equal(sortedSet(before.SupportedTypes), sortedSet(supportedTypes))
}

// validateHello returns what is wrong with a hello, or "" when it is fine. instanceID is the one
// the request's header names, which the body must repeat.
func validateHello(req *probeproto.HelloRequest, instanceID string) string {
	switch {
	case req.InstanceID != instanceID:
		return "instance_id in the body must be the " + probeproto.HeaderInstanceID + " header's value"
	case len(req.Hostname) > maxHostnameLength:
		return "hostname is too long: at most " + strconv.Itoa(maxHostnameLength) + " characters"
	case len(req.OS) > maxOSLength:
		return "os is too long: at most " + strconv.Itoa(maxOSLength) + " characters"
	case len(req.Arch) > maxArchLength:
		return "arch is too long: at most " + strconv.Itoa(maxArchLength) + " characters"
	case len(req.ProbeVersion) > maxProbeVersionLength:
		return "probe_version is too long: at most " + strconv.Itoa(maxProbeVersionLength) + " characters"
	case len(req.SupportedTypes) > maxSupportedTypes:
		return "supported_types holds too many monitor types: at most " + strconv.Itoa(maxSupportedTypes)
	}
	for _, monitorType := range req.SupportedTypes {
		if monitorType == "" || len(monitorType) > maxMonitorTypeLength {
			return "supported_types must hold monitor types of 1 to " + strconv.Itoa(maxMonitorTypeLength) + " characters"
		}
	}
	return ""
}
