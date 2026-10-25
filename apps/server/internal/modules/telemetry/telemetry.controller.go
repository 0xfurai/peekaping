package telemetry

import (
	"context"
	"net/http"

	"peekaping/internal/httpx"
	"peekaping/internal/utils"
	"peekaping/internal/validation"

	"go.uber.org/zap"
)

type Controller struct {
	service Service
	logger  *zap.SugaredLogger
}

func NewController(
	service Service,
	logger *zap.SugaredLogger,
) *Controller {
	return &Controller{
		service,
		logger.Named("[telemetry-controller]"),
	}
}

func (c *Controller) Get(ctx context.Context, _ *GetInput) (*httpx.Output[StateResponse], error) {
	state, err := c.service.State(ctx)
	if err != nil {
		c.logger.Errorw("Failed to read the telemetry state", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusOK, newStateResponse(state))
}

func (c *Controller) MarkNotice(ctx context.Context, _ *MarkNoticeInput) (*httpx.Output[StateResponse], error) {
	state, err := c.service.MarkNoticeSeen(ctx)
	if err != nil {
		c.logger.Errorw("Failed to mark the telemetry notice seen", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusOK, newStateResponse(state))
}

func (c *Controller) Set(ctx context.Context, input *SetInput) (*httpx.Output[StateResponse], error) {
	dto, err := input.Decode()
	if err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}
	if err := validation.Validate.Struct(dto); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}
	state, err := c.service.SetEnabled(ctx, *dto.Enabled)
	if err != nil {
		c.logger.Errorw("Failed to change the telemetry choice", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusOK, newStateResponse(state))
}
