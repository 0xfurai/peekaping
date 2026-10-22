package probe

import (
	"context"
	"errors"
	"net/http"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/probe_notification"
	"peekaping/internal/utils"

	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
)

type Controller struct {
	service Service
	logger  *zap.SugaredLogger
}

func NewController(service Service, logger *zap.SugaredLogger) *Controller {
	return &Controller{
		service: service,
		logger:  logger.Named("[probe-controller]"),
	}
}

// The reasons the local probe can't be changed, which the 409 answers give
const (
	localProbeCantBeDeleted  = "The local probe can't be deleted: it runs the checks Peekaping makes itself"
	localProbeCantBeDisabled = "The local probe can't be disabled: it runs the checks Peekaping makes itself"
	localProbeCantBeRotated  = "The local probe's token is set with PROBE_BOOTSTRAP_TOKEN: set or change it and restart Peekaping"
)

// fail turns a service error into the response that tells the caller what went wrong.
// localMessage says why the local probe can't be changed the way the request asked.
func (c *Controller) fail(err error, localMessage string) error {
	switch {
	case errors.Is(err, ErrUnsupportedStorage):
		return httpx.Fail(http.StatusNotImplemented, utils.NewFailResponse(err.Error()))
	case errors.Is(err, ErrNotFound):
		return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Probe not found"))
	case errors.Is(err, ErrLocalProbe):
		return httpx.Fail(http.StatusConflict, utils.NewFailResponse(localMessage))
	case errors.Is(err, probe_notification.ErrUnknownChannel):
		return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}
	c.logger.Errorw("Probe request failed", "error", err)
	return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
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

	created, err := c.service.Create(ctx, &CreateRequest{
		Name:            req.Name,
		Location:        req.Location,
		NotificationIDs: req.NotificationIDs,
	})
	if err != nil {
		return nil, c.fail(err, "")
	}

	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("Probe created successfully. Copy its token now: it is shown only once", ToProbeWithTokenResponse(created)))
}

// GetProbes lists the probes, the local probe first. A MongoDB install has none.
func (c *Controller) GetProbes(ctx context.Context, input *GetProbesInput) (*httpx.Output[utils.ApiResponse[[]*ProbeResponse]], error) {
	// MARK: GetProbes

	probes, err := c.service.FindAll(ctx)
	if err != nil {
		return nil, c.fail(err, "")
	}

	responses := make([]*ProbeResponse, len(probes))
	for i, p := range probes {
		responses[i] = ToProbeResponse(p)
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

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Probe retrieved successfully", ToProbeResponse(found)))
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

	updated, err := c.service.Update(ctx, r.PathValue("id"), &UpdateRequest{
		Name:            req.Name,
		Location:        req.Location,
		Disabled:        req.Disabled,
		NotificationIDs: req.NotificationIDs,
	})
	if err != nil {
		return nil, c.fail(err, localProbeCantBeDisabled)
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Probe updated successfully", ToProbeResponse(updated)))
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
