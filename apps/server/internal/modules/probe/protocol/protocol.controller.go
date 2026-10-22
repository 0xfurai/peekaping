package protocol

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/probe"
	"peekaping/internal/probeproto"
	"peekaping/internal/utils"
	"peekaping/internal/version"

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
	service probe.Service
	logger  *zap.SugaredLogger
}

func NewController(service probe.Service, logger *zap.SugaredLogger) *Controller {
	return &Controller{
		service: service,
		logger:  logger.Named("[probe-protocol-controller]"),
	}
}

// fail turns a service error into the response that tells the probe what went wrong
func (c *Controller) fail(err error) error {
	switch {
	case errors.Is(err, probe.ErrUnsupportedStorage):
		return httpx.Fail(http.StatusNotImplemented, utils.NewFailResponse(err.Error()))
	case errors.Is(err, probe.ErrInstanceTaken):
		return httpx.Fail(http.StatusConflict, utils.NewFailResponse("this instance id belongs to another probe: use a new one"))
	}
	c.logger.Errorw("Probe protocol request failed", "error", err)
	return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse(messageInternalError))
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

	err := c.service.RegisterInstance(r.Context(), owner.ID, &probe.Instance{
		ID:              instanceID,
		Hostname:        req.Hostname,
		OS:              req.OS,
		Arch:            req.Arch,
		Version:         req.ProbeVersion,
		ProtocolVersion: req.ProtocolVersion,
		LastSeenAt:      time.Now().UTC(),
	}, req.SupportedTypes)
	if err != nil {
		return nil, c.fail(err)
	}

	return httpx.Reply(http.StatusOK, probeproto.HelloResponse{
		ProtocolVersion: probeproto.ProtocolVersion,
		ServerVersion:   version.Version,
		ServerTime:      time.Now().UTC(),
		Probe:           probeproto.ProbeInfo{ID: owner.ID, Name: owner.Name, Location: owner.Location},
		PollTimeoutMs:   int(PollTimeout / time.Millisecond),
		FlushIntervalMs: int(FlushInterval / time.Millisecond),
		BatchMax:        BatchMax,
		BufferMax:       BufferMax,
	})
}

// Bye removes the instance that says it is stopping
func (c *Controller) Bye(ctx context.Context, input *ByeInput) (*httpx.Output[httpx.NoBody], error) {
	// MARK: Bye

	r := input.Request
	owner, instanceID, ok := ProbeFromContext(r.Context())
	if !ok {
		return nil, c.unauthenticated()
	}
	if err := c.service.RemoveInstance(r.Context(), owner.ID, instanceID); err != nil {
		return nil, c.fail(err)
	}

	return httpx.Reply(http.StatusNoContent, httpx.NoBody{})
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
