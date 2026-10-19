package tag

import (
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/utils"

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

// @Router		/tags [get]
// @Summary		Get tags
// @Tags			Tags
// @Produce		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     q    query     string  false  "Search query"
// @Param     page query     int     false  "Page number" default(1)
// @Param     limit query    int     false  "Items per page" default(10) minimum(1) maximum(1000)
// @Success		200	{object}	utils.ApiResponse[[]Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) FindAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	page, err := utils.GetQueryInt(r, "page", 0)
	if err != nil || page < 0 {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse("Invalid page parameter"))
		return
	}

	limit, err := utils.GetQueryInt(r, "limit", 10)
	if err != nil || limit < 1 || limit > utils.MaxPageSize {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse("Invalid limit parameter"))
		return
	}

	q := r.URL.Query().Get("q")

	response, err := c.service.FindAll(ctx, page, limit, q)
	if err != nil {
		c.logger.Errorw("Failed to fetch tags", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("success", response))
}

// @Router		/tags [post]
// @Summary		Create tag
// @Tags			Tags
// @Produce		json
// @Accept		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     body body   CreateUpdateDto  true  "Tag object"
// @Success		201	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var tag *CreateUpdateDto
	if err := httpx.DecodeJSON(r, &tag); err != nil {
		c.logger.Errorw("Invalid request body", "error", err)
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
		return
	}

	if err := utils.Validate.Struct(tag); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	createdTag, err := c.service.Create(ctx, tag)
	if err != nil {
		c.logger.Errorw("Failed to create tag", "error", err)
		if err.Error() == "tag with this name already exists" {
			httpx.JSON(w, http.StatusConflict, utils.NewFailResponse("Tag with this name already exists"))
			return
		}
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	httpx.JSON(w, http.StatusCreated, utils.NewSuccessResponse("Tag created successfully", createdTag))
}

// @Router		/tags/{id} [get]
// @Summary		Get tag by ID
// @Tags			Tags
// @Produce		json
// @Security BearerAuth
// @Param       id   path      string  true  "Tag ID"
// @Success		200	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) FindByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	tag, err := c.service.FindByID(ctx, id)
	if err != nil {
		c.logger.Errorw("Failed to fetch tag", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	if tag == nil {
		httpx.JSON(w, http.StatusNotFound, utils.NewFailResponse("Tag not found"))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("success", tag))
}

// @Router		/tags/{id} [put]
// @Summary		Update tag
// @Tags			Tags
// @Produce		json
// @Accept		json
// @Security BearerAuth
// @Param       id   path      string  true  "Tag ID"
// @Param       tag body     CreateUpdateDto  true  "Tag object"
// @Success		200	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) UpdateFull(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	var tag CreateUpdateDto
	if err := httpx.DecodeJSON(r, &tag); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := utils.Validate.Struct(tag); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if _, ok := utils.FindOr404(ctx, w, c.logger, "Tag", c.service.FindByID, id); !ok {
		return
	}

	updatedTag, err := c.service.UpdateFull(ctx, id, &tag)
	if err != nil {
		c.logger.Errorw("Failed to update tag", "error", err)
		if err.Error() == "tag with this name already exists" {
			httpx.JSON(w, http.StatusConflict, utils.NewFailResponse("Tag with this name already exists"))
			return
		}
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("Tag updated successfully", updatedTag))
}

// @Router		/tags/{id} [patch]
// @Summary		Update tag
// @Tags			Tags
// @Produce		json
// @Accept		json
// @Security BearerAuth
// @Param       id   path      string  true  "Tag ID"
// @Param       tag body     PartialUpdateDto  true  "Tag object"
// @Success		200	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) UpdatePartial(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	var tag PartialUpdateDto
	if err := httpx.DecodeJSON(r, &tag); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := utils.Validate.Struct(tag); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if _, ok := utils.FindOr404(ctx, w, c.logger, "Tag", c.service.FindByID, id); !ok {
		return
	}

	updatedTag, err := c.service.UpdatePartial(ctx, id, &tag)
	if err != nil {
		c.logger.Errorw("Failed to update tag", "error", err)
		if err.Error() == "tag with this name already exists" {
			httpx.JSON(w, http.StatusConflict, utils.NewFailResponse("Tag with this name already exists"))
			return
		}
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("Tag updated successfully", updatedTag))
}

// @Router		/tags/{id} [delete]
// @Summary		Delete tag
// @Tags			Tags
// @Produce		json
// @Security BearerAuth
// @Param       id   path      string  true  "Tag ID"
// @Success		200	{object}	utils.ApiResponse[any]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	if _, ok := utils.FindOr404(ctx, w, c.logger, "Tag", c.service.FindByID, id); !ok {
		return
	}

	err := c.service.Delete(ctx, id)
	if err != nil {
		c.logger.Errorw("Failed to delete tag", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse[any]("Tag deleted successfully", nil))
}
