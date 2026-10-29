package probe

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/middleware"
	"peekaping/internal/modules/probe_notification"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/utils"

	"github.com/go-playground/validator/v10"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// MonitorAssigner assigns a probe to monitors. The monitor service stands behind it, wired in
// internal/app, so that the probe module doesn't import the monitor module: the monitor service
// already depends on the probe service.
type MonitorAssigner interface {
	// AssignProbeToAll adds the probe to every monitor, paused ones included but push monitors
	// not, of one of the monitor types (every type when there are none) that doesn't run on it,
	// and returns how many it added
	AssignProbeToAll(ctx context.Context, probeID string, supportedTypes []string) (int, error)
}

// InstanceAdmins says whether a request comes from an instance admin. middleware.AuthChain is one.
type InstanceAdmins interface {
	IsInstanceAdmin(ctx context.Context) (bool, error)
}

type Controller struct {
	service      Service
	admins       InstanceAdmins
	availability AvailabilityService
	monitors     MonitorAssigner
	liveness     shared.LocalProbeLiveness
	workspaces   workspace.Repository
	logger       *zap.SugaredLogger
}

// NewController makes the controller. monitors assigns the probe to monitors. liveness says whether
// the local probe follows its stored status or is up as long as Peekaping runs. workspaces tells
// which workspaces a caller is a member of, since a probe is linked only to channels of those
// (docs/adr/0005 section 10). admins says who is an instance admin: the others see a probe without
// its instances' hosts and its channels.
func NewController(service Service, admins InstanceAdmins, availability AvailabilityService, monitors MonitorAssigner, liveness shared.LocalProbeLiveness, workspaces workspace.Repository, logger *zap.SugaredLogger) *Controller {
	return &Controller{
		service:      service,
		admins:       admins,
		availability: availability,
		monitors:     monitors,
		liveness:     liveness,
		workspaces:   workspaces,
		logger:       logger.Named("[probe-controller]"),
	}
}

// controllerParams are what the install's controller is built from
type controllerParams struct {
	dig.In

	Service      Service
	Admins       *middleware.AuthChain
	Availability AvailabilityService
	// Monitors assigns the probe to monitors: the monitor service, wired in internal/app
	Monitors MonitorAssigner
	// LocalProbe is how the local probe is known to be up: the process itself when the install
	// provides none
	LocalProbe shared.LocalProbeLiveness `optional:"true"`
	Workspaces workspace.Repository
	Logger     *zap.SugaredLogger
}

func provideController(p controllerParams) *Controller {
	return NewController(p.Service, p.Admins, p.Availability, p.Monitors, p.LocalProbe, p.Workspaces, p.Logger)
}

// response maps a probe to its response. The local probe in single mode is online whatever status
// is stored: the stored one is left as it is, for a move to cluster mode.
func (c *Controller) response(m *Model) *ProbeResponse {
	response := ToProbeResponse(m)
	if !c.liveness.FollowsItsStatus(m.ID) {
		response.Status = string(StatusOnline)
	}
	return response
}

// view maps a probe to the response its reader may see. An instance admin sees all of it. A member
// or an API key sees the probe's name, location, status and monitor types, and not its instances'
// hosts or its channels (docs/adr/0005 section 10).
func (c *Controller) view(ctx context.Context, m *Model) (*ProbeResponse, error) {
	admin, err := c.admins.IsInstanceAdmin(ctx)
	if err != nil {
		return nil, err
	}
	response := c.response(m)
	if !admin {
		response.WithoutAdminDetails()
	}
	return response, nil
}

// The reasons the local probe can't be changed, which the 409 answers give
const (
	localProbeCantBeDeleted  = "The local probe can't be deleted: it runs the checks Peekaping makes itself"
	localProbeCantBeDisabled = "The local probe can't be disabled: it runs the checks Peekaping makes itself"
	localProbeCantBeAssigned = "The local probe can't be assigned every monitor: it runs the checks Peekaping makes itself, and a monitor is moved to it by editing the monitor"
	localProbeCantBeRotated  = "The local probe's token can't be rotated. In cluster mode it is set with PROBE_BOOTSTRAP_TOKEN: set or change it and restart Peekaping. In single mode the local probe takes no token: the peekaping process checks its monitors"
)

// probeHasMonitors is why a probe with monitors on it can't be deleted
func probeHasMonitors(monitors int) string {
	runs := "monitors run"
	if monitors == 1 {
		runs = "monitor runs"
	}
	return fmt.Sprintf("The probe can't be deleted: %d %s on it. Move them to another probe or delete them first", monitors, runs)
}

// fail turns a service error into the response that tells the caller what went wrong.
// localMessage says why the local probe can't be changed the way the request asked.
func (c *Controller) fail(err error, localMessage string) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Probe not found"))
	case errors.Is(err, ErrLocalProbe):
		return httpx.Fail(http.StatusConflict, utils.NewFailResponse(localMessage))
	case errors.Is(err, ErrProbeHasMonitors):
		var hasMonitors *HasMonitorsError
		if errors.As(err, &hasMonitors) {
			return httpx.Fail(http.StatusConflict, utils.NewFailResponse(probeHasMonitors(hasMonitors.Monitors)))
		}
		return httpx.Fail(http.StatusConflict, utils.NewFailResponse(err.Error()))
	case errors.Is(err, probe_notification.ErrUnknownChannel):
		return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}
	c.logger.Errorw("Probe request failed", "error", err)
	return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
}

// channelWorkspaces returns the workspaces whose channels the caller may link to a probe: the ones
// the signed-in instance admin is a member of. A probe belongs to the install, and the channels it
// notifies are those of a workspace its manager is in (docs/adr/0005 section 10), so a channel of
// any other workspace is refused as one that doesn't exist. It reads nothing when the request names
// no channel. Only an instance admin gets this far, and an API key is never one.
func (c *Controller) channelWorkspaces(ctx context.Context, notificationIDs []string) ([]workspace.ID, error) {
	if len(notificationIDs) == 0 {
		return nil, nil
	}
	userID, _ := httpx.Value(ctx, "userId").(string)
	places, err := c.workspaces.ListMemberships(ctx, userID)
	if err != nil {
		return nil, err
	}
	member := make([]workspace.ID, len(places))
	for i, place := range places {
		member[i] = place.Workspace.ID
	}
	return member, nil
}

// CreateProbe creates a probe and answers with its token, once
func (c *Controller) CreateProbe(ctx context.Context, input *CreateProbeInput) (*httpx.Output[utils.ApiResponse[*ProbeWithTokenResponse]], error) {
	// MARK: CreateProbe

	req, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}
	if err := validator.New().Struct(req); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	channelWorkspaces, err := c.channelWorkspaces(ctx, req.NotificationIDs)
	if err != nil {
		return nil, c.fail(err, "")
	}
	created, err := c.service.Create(ctx, &CreateRequest{
		Name:              req.Name,
		Location:          req.Location,
		NotificationIDs:   req.NotificationIDs,
		ChannelWorkspaces: channelWorkspaces,
		AssignAllMonitors: req.AssignAllMonitors,
	})
	if err != nil {
		return nil, c.fail(err, "")
	}

	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("Probe created successfully. Copy its token now: it is shown only once", ToProbeWithTokenResponse(created)))
}

// GetProbes lists the probes, the local probe first
func (c *Controller) GetProbes(ctx context.Context, input *GetProbesInput) (*httpx.Output[utils.ApiResponse[[]*ProbeResponse]], error) {
	// MARK: GetProbes

	probes, err := c.service.FindAll(ctx)
	if err != nil {
		return nil, c.fail(err, "")
	}

	// One read of the reader's flag for the whole list
	admin, err := c.admins.IsInstanceAdmin(ctx)
	if err != nil {
		return nil, c.fail(err, "")
	}
	responses := make([]*ProbeResponse, len(probes))
	for i, p := range probes {
		responses[i] = c.response(p)
		if !admin {
			responses[i].WithoutAdminDetails()
		}
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Probes retrieved successfully", responses))
}

// GetProbe gets a probe by its ID
func (c *Controller) GetProbe(ctx context.Context, input *GetProbeInput) (*httpx.Output[utils.ApiResponse[*ProbeResponse]], error) {
	r := input.Request
	// MARK: GetProbe

	found, err := c.service.FindByID(ctx, r.PathValue("id"))
	if err != nil {
		return nil, c.fail(err, "")
	}
	if found == nil {
		return nil, c.fail(ErrNotFound, "")
	}

	response, err := c.view(ctx, found)
	if err != nil {
		return nil, c.fail(err, "")
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Probe retrieved successfully", response))
}

// UpdateProbe updates a probe's name, location, disabled flag and notification channels
func (c *Controller) UpdateProbe(ctx context.Context, input *UpdateProbeInput) (*httpx.Output[utils.ApiResponse[*ProbeResponse]], error) {
	r := input.Request
	// MARK: UpdateProbe

	req, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}
	if err := validator.New().Struct(req); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	channelWorkspaces, err := c.channelWorkspaces(ctx, req.NotificationIDs)
	if err != nil {
		return nil, c.fail(err, localProbeCantBeDisabled)
	}
	updated, err := c.service.Update(ctx, r.PathValue("id"), &UpdateRequest{
		Name:              req.Name,
		Location:          req.Location,
		Disabled:          req.Disabled,
		IsDefault:         req.IsDefault,
		NotificationIDs:   req.NotificationIDs,
		ChannelWorkspaces: channelWorkspaces,
	})
	if err != nil {
		return nil, c.fail(err, localProbeCantBeDisabled)
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Probe updated successfully", c.response(updated)))
}

// DeleteProbe deletes a probe
func (c *Controller) DeleteProbe(ctx context.Context, input *DeleteProbeInput) (*httpx.Output[httpx.NoBody], error) {
	r := input.Request
	// MARK: DeleteProbe

	if err := c.service.Delete(ctx, r.PathValue("id")); err != nil {
		return nil, c.fail(err, localProbeCantBeDeleted)
	}

	return httpx.Reply(http.StatusNoContent, httpx.NoBody{})
}

// RotateProbeToken replaces a probe's token and answers with the new one, once
func (c *Controller) RotateProbeToken(ctx context.Context, input *RotateProbeTokenInput) (*httpx.Output[utils.ApiResponse[*ProbeWithTokenResponse]], error) {
	r := input.Request
	// MARK: RotateProbeToken

	rotated, err := c.service.RotateToken(ctx, r.PathValue("id"))
	if err != nil {
		return nil, c.fail(err, localProbeCantBeRotated)
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Probe token rotated successfully. Copy the new token now: it is shown only once, and the old one no longer works", ToProbeWithTokenResponse(rotated)))
}

// AssignAllMonitors assigns a probe every monitor it can run now, and answers with how many it added.
// A probe with no instance on record doesn't know what it runs yet, so it is made to wait for its
// first hello instead.
func (c *Controller) AssignAllMonitors(ctx context.Context, input *AssignAllMonitorsInput) (*httpx.Output[utils.ApiResponse[*AssignAllMonitorsResponse]], error) {
	r := input.Request
	// MARK: AssignAllMonitors

	id := r.PathValue("id")
	found, err := c.service.FindByID(ctx, id)
	if err != nil {
		return nil, c.fail(err, "")
	}
	if found == nil {
		return nil, c.fail(ErrNotFound, "")
	}
	if id == LocalID {
		return nil, c.fail(ErrLocalProbe, localProbeCantBeAssigned)
	}

	if len(found.Instances) == 0 {
		if err := c.service.SetPendingAssignment(ctx, id); err != nil {
			return nil, c.fail(err, "")
		}

		// Of a first hello and this action racing, whichever runs second sees the other's write: the
		// hello claims the flag after its instance is committed, and this read comes after the flag
		// is committed. So an instance that registered after the read above, whose hello didn't see
		// the flag, shows up here, and is assigned now. If both assign, the second adds nothing: a
		// probe isn't added to a monitor twice.
		again, err := c.service.FindByID(ctx, id)
		if err != nil {
			return nil, c.fail(err, "")
		}
		if again == nil {
			return nil, c.fail(ErrNotFound, "")
		}
		if len(again.Instances) == 0 {
			return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("The probe is assigned every monitor when its first instance says hello", &AssignAllMonitorsResponse{Pending: true}))
		}
		found = again
	}

	// FindByID made the probe's types those of its online instances, or the last it had while
	// none is online
	added, err := c.monitors.AssignProbeToAll(ctx, id, found.SupportedTypes)
	if err != nil {
		return nil, c.fail(err, "")
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Monitors assigned successfully", &AssignAllMonitorsResponse{Added: added}))
}

// availabilityRange reads the range query, 24h when it is left out
func availabilityRange(r *http.Request) (AvailabilityRange, error) {
	rng, ok := ParseAvailabilityRange(r.URL.Query().Get("range"))
	if !ok {
		return "", httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid range parameter (must be 24h, 7d or 30d)"))
	}
	return rng, nil
}

// GetProbesAvailability gets every probe's online and offline periods and uptime, without their
// instances, the local probe first
func (c *Controller) GetProbesAvailability(ctx context.Context, input *GetProbesAvailabilityInput) (*httpx.Output[utils.ApiResponse[[]*ProbeAvailabilityResponse]], error) {
	// MARK: GetProbesAvailability

	rng, err := availabilityRange(input.Request)
	if err != nil {
		return nil, err
	}
	all, err := c.availability.AvailabilityOfAll(ctx, rng)
	if err != nil {
		return nil, c.fail(err, "")
	}

	responses := make([]*ProbeAvailabilityResponse, len(all))
	for i, a := range all {
		responses[i] = ToProbeAvailabilityResponse(a, false)
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Probe availability retrieved successfully", responses))
}

// GetProbeAvailability gets a probe's online and offline periods and uptime, with its instances
// and when each was connected over the last 24 hours
func (c *Controller) GetProbeAvailability(ctx context.Context, input *GetProbeAvailabilityInput) (*httpx.Output[utils.ApiResponse[*ProbeAvailabilityResponse]], error) {
	r := input.Request
	// MARK: GetProbeAvailability

	rng, err := availabilityRange(r)
	if err != nil {
		return nil, err
	}
	found, err := c.availability.Availability(ctx, r.PathValue("id"), rng)
	if err != nil {
		return nil, c.fail(err, "")
	}

	response := ToProbeAvailabilityResponse(found, true)
	admin, err := c.admins.IsInstanceAdmin(ctx)
	if err != nil {
		return nil, c.fail(err, "")
	}
	if !admin {
		response.WithoutAdminDetails()
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Probe availability retrieved successfully", response))
}
