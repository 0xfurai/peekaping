package tag

import (
	"net/http"
	"vigi/internal/utils"

	"github.com/gin-gonic/gin"
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
// @Param     limit query    int     false  "Items per page" default(10)
// @Success		200	{object}	utils.ApiResponse[[]Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) FindAll(ctx *gin.Context) {
	page, err := utils.GetQueryInt(ctx, "page", 0)
	if err != nil || page < 0 {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid page parameter"))
		return
	}

	limit, err := utils.GetQueryInt(ctx, "limit", 10)
	if err != nil || limit < 1 {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid limit parameter"))
		return
	}

	q := ctx.Query("q")

	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	response, err := c.service.FindAll(ctx, page, limit, q, orgID)
	if err != nil {
		c.logger.Errorw("Failed to fetch tags", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", response))
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
func (c *Controller) Create(ctx *gin.Context) {
	var tag *CreateUpdateDto
	if err := ctx.ShouldBindJSON(&tag); err != nil {
		c.logger.Errorw("Invalid request body", "error", err)
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
		return
	}

	if err := utils.Validate.Struct(tag); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	createdTag, err := c.service.Create(ctx, tag, orgID)
	if err != nil {
		c.logger.Errorw("Failed to create tag", "error", err)
		if err.Error() == "tag with this name already exists" {
			ctx.JSON(http.StatusConflict, utils.NewFailResponse("Tag with this name already exists"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusCreated, utils.NewSuccessResponse("Tag created successfully", createdTag))
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
func (c *Controller) FindByID(ctx *gin.Context) {
	id := ctx.Param("id")

	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	tag, err := c.service.FindByID(ctx, id, orgID)
	if err != nil {
		c.logger.Errorw("Failed to fetch tag", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	if tag == nil {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Tag not found"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", tag))
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
func (c *Controller) UpdateFull(ctx *gin.Context) {
	id := ctx.Param("id")

	var tag CreateUpdateDto
	if err := ctx.ShouldBindJSON(&tag); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := utils.Validate.Struct(tag); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	updatedTag, err := c.service.UpdateFull(ctx, id, &tag, orgID)
	if err != nil {
		c.logger.Errorw("Failed to update tag", "error", err)
		if err.Error() == "tag with this name already exists" {
			ctx.JSON(http.StatusConflict, utils.NewFailResponse("Tag with this name already exists"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("Tag updated successfully", updatedTag))
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
func (c *Controller) UpdatePartial(ctx *gin.Context) {
	id := ctx.Param("id")

	var tag PartialUpdateDto
	if err := ctx.ShouldBindJSON(&tag); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := utils.Validate.Struct(tag); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	updatedTag, err := c.service.UpdatePartial(ctx, id, &tag, orgID)
	if err != nil {
		c.logger.Errorw("Failed to update tag", "error", err)
		if err.Error() == "tag with this name already exists" {
			ctx.JSON(http.StatusConflict, utils.NewFailResponse("Tag with this name already exists"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("Tag updated successfully", updatedTag))
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
func (c *Controller) Delete(ctx *gin.Context) {
	id := ctx.Param("id")

	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	err := c.service.Delete(ctx, id, orgID)
	if err != nil {
		c.logger.Errorw("Failed to delete tag", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse[any]("Tag deleted successfully", nil))
}
