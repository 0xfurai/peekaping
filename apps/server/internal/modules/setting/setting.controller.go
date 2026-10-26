package setting

import (
	"context"
	"errors"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/utils"
	"peekaping/internal/validation"

	"go.uber.org/zap"
)

type Controller struct {
	service APIService
	logger  *zap.SugaredLogger
}

func NewController(
	service APIService,
	logger *zap.SugaredLogger,
) *Controller {
	return &Controller{
		service,
		logger,
	}
}
func (ic *Controller) GetByKey(ctx context.Context, input *GetByKeyInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	key := r.PathValue("key")
	entity, err := ic.service.GetByKey(ctx, key)
	if err != nil {
		ic.logger.Errorw("Failed to fetch setting by key", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	if entity == nil {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Setting not found"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", entity))
}
func (ic *Controller) SetByKey(ctx context.Context, input *SetByKeyInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	key := r.PathValue("key")
	// Check the key before the body, so an internal key answers 404 whatever
	// the body holds.
	if !ic.service.IsUserEditable(key) {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Setting not found"))
	}
	entity, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}
	if err := validation.Validate.Struct(entity); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}
	updated, err := ic.service.SetByKey(ctx, key, &Model{Value: entity.Value, Type: entity.Type})
	if errors.Is(err, ErrSettingNotFound) {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Setting not found"))
	}
	if errors.Is(err, ErrInvalidValue) {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}
	if err != nil {
		ic.logger.Errorw("Failed to set setting by key", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("setting set successfully", updated))
}
func (ic *Controller) DeleteByKey(ctx context.Context, input *DeleteByKeyInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	r := input.Request
	key := r.PathValue("key")
	err := ic.service.DeleteByKey(ctx, key)
	if errors.Is(err, ErrSettingNotFound) {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Setting not found"))
	}
	if err != nil {
		ic.logger.Errorw("Failed to delete setting by key", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Setting deleted successfully", nil))
}
