package proxy

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
	// Register custom struct-level validation if needed
	// validate.RegisterStructValidation(CreateUpdateDtoStructLevelValidation, CreateUpdateDto{})
	return &Controller{
		service,
		logger,
	}
}
func (ic *Controller) FindAll(ctx context.Context, input *FindAllInput) (*httpx.Output[utils.ApiResponse[[]*Model]], error) {
	r := input.Request
	page, err := utils.GetQueryInt(r, "page", 0)
	if err != nil || page < 0 {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid page parameter"))
	}

	limit, err := utils.GetQueryInt(r, "limit", 10)
	if err != nil || limit < 1 || limit > utils.MaxPageSize {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid limit parameter"))
	}

	q := r.URL.Query().Get("q")

	entities, err := ic.service.FindAll(ctx, page, limit, q)
	if err != nil {
		ic.logger.Errorw("Failed to fetch proxies", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", entities))
}
func (ic *Controller) Create(ctx context.Context, input *CreateInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	entity, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := validation.Validate.Struct(entity); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	created, err := ic.service.Create(ctx, entity)
	if err != nil {
		ic.logger.Errorw("Failed to create proxy", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("Proxy created successfully", created))
}
func (ic *Controller) FindByID(ctx context.Context, input *FindByIDInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	id := r.PathValue("id")

	entity, err := ic.service.FindByID(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to fetch proxy", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	if entity == nil {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Proxy not found"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", entity))
}
func (ic *Controller) UpdateFull(ctx context.Context, input *UpdateFullInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	id := r.PathValue("id")

	entity, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}

	if err := validation.Validate.Struct(entity); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if _, err := utils.RequireRecord(ctx, ic.logger, "Proxy", ic.service.FindByID, id); err != nil {
		return nil, err
	}

	updated, err := ic.service.UpdateFull(ctx, id, &entity)
	if err != nil {
		ic.logger.Errorw("Failed to update proxy", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("proxy updated successfully", updated))
}
func (ic *Controller) UpdatePartial(ctx context.Context, input *UpdatePartialInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	id := r.PathValue("id")

	entity, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}

	if err := validation.Validate.Struct(entity); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if _, err := utils.RequireRecord(ctx, ic.logger, "Proxy", ic.service.FindByID, id); err != nil {
		return nil, err
	}

	updated, err := ic.service.UpdatePartial(ctx, id, &entity)
	if err != nil {
		ic.logger.Errorw("Failed to update proxy", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("proxy updated successfully", updated))
}
func (ic *Controller) Delete(ctx context.Context, input *DeleteInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	r := input.Request
	id := r.PathValue("id")

	if _, err := utils.RequireRecord(ctx, ic.logger, "Proxy", ic.service.FindByID, id); err != nil {
		return nil, err
	}

	err := ic.service.Delete(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to delete proxy", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Proxy deleted successfully", nil))
}
