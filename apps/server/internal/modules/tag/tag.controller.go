package tag

import (
	"context"
	"errors"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacehttp"
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
		logger,
	}
}
func (c *Controller) FindAll(ctx context.Context, input *FindAllInput) (*httpx.Output[utils.ApiResponse[[]*Model]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
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

	response, err := c.service.FindAll(ctx, workspaceID, page, limit, q)
	if err != nil {
		c.logger.Errorw("Failed to fetch tags", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", response))
}
func (c *Controller) Create(ctx context.Context, input *CreateInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	tag, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		c.logger.Errorw("Invalid request body", "error", err)
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}

	if err := validation.Validate.Struct(tag); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	createdTag, err := c.service.Create(ctx, workspaceID, tag)
	if err != nil {
		c.logger.Errorw("Failed to create tag", "error", err)
		if errors.Is(err, ErrNameTaken) {
			return nil, httpx.Fail(http.StatusConflict, utils.NewFailResponse("Tag with this name already exists"))
		}
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("Tag created successfully", createdTag))
}
func (c *Controller) FindByID(ctx context.Context, input *FindByIDInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	tag, err := c.service.FindByID(ctx, workspaceID, id)
	if err != nil {
		c.logger.Errorw("Failed to fetch tag", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	if tag == nil {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Tag not found"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", tag))
}
func (c *Controller) UpdateFull(ctx context.Context, input *UpdateFullInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	tag, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := validation.Validate.Struct(tag); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if _, err := utils.RequireRecord(ctx, c.logger, "Tag", findIn(c.service, workspaceID), id); err != nil {
		return nil, err
	}

	updatedTag, err := c.service.UpdateFull(ctx, workspaceID, id, &tag)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, tagNotFoundResponse()
		}
		c.logger.Errorw("Failed to update tag", "error", err)
		if errors.Is(err, ErrNameTaken) {
			return nil, httpx.Fail(http.StatusConflict, utils.NewFailResponse("Tag with this name already exists"))
		}
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Tag updated successfully", updatedTag))
}
func (c *Controller) UpdatePartial(ctx context.Context, input *UpdatePartialInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	tag, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := validation.Validate.Struct(tag); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if _, err := utils.RequireRecord(ctx, c.logger, "Tag", findIn(c.service, workspaceID), id); err != nil {
		return nil, err
	}

	updatedTag, err := c.service.UpdatePartial(ctx, workspaceID, id, &tag)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, tagNotFoundResponse()
		}
		c.logger.Errorw("Failed to update tag", "error", err)
		if errors.Is(err, ErrNameTaken) {
			return nil, httpx.Fail(http.StatusConflict, utils.NewFailResponse("Tag with this name already exists"))
		}
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Tag updated successfully", updatedTag))
}
func (c *Controller) Delete(ctx context.Context, input *DeleteInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	if _, err := utils.RequireRecord(ctx, c.logger, "Tag", findIn(c.service, workspaceID), id); err != nil {
		return nil, err
	}

	err = c.service.Delete(ctx, workspaceID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, tagNotFoundResponse()
		}
		c.logger.Errorw("Failed to delete tag", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Tag deleted successfully", nil))
}

// findIn is the lookup of a tag in the workspace of the request: a tag of another workspace is not
// found, as one that doesn't exist is
func findIn(service Service, workspaceID workspace.ID) func(context.Context, string) (*Model, error) {
	return func(ctx context.Context, id string) (*Model, error) {
		return service.FindByID(ctx, workspaceID, id)
	}
}

// tagNotFoundResponse is the answer for a tag that doesn't exist and for one that belongs to
// another workspace, which a caller can't tell apart
func tagNotFoundResponse() error {
	return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Tag not found"))
}
