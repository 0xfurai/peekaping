package proxy

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
	// Register custom struct-level validation if needed
	// validate.RegisterStructValidation(CreateUpdateDtoStructLevelValidation, CreateUpdateDto{})
	return &Controller{
		service,
		logger,
	}
}

// @Router		/proxies [get]
// @Summary		Get proxies
// @Tags			Proxies
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
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) FindAll(ctx *gin.Context) {
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
	orgID := ctx.GetString("orgId")

	entities, err := ic.service.FindAll(ctx, page, limit, q, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to fetch proxies", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", entities))
}

// @Router		/proxies [post]
// @Summary		Create proxy
// @Tags			Proxies
// @Produce		json
// @Accept		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     body body   CreateUpdateDto  true  "Proxy object"
// @Success		201	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) Create(ctx *gin.Context) {
	var entity *CreateUpdateDto
	if err := ctx.ShouldBindJSON(&entity); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := utils.Validate.Struct(entity); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	entity.OrgID = ctx.GetString("orgId")

	created, err := ic.service.Create(ctx, entity)
	if err != nil {
		ic.logger.Errorw("Failed to create proxy", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusCreated, utils.NewSuccessResponse("Proxy created successfully", created))
}

// @Router		/proxies/{id} [get]
// @Summary		Get proxy by ID
// @Tags			Proxies
// @Produce		json
// @Security BearerAuth
// @Param       id   path      string  true  "Proxy ID"
// @Success		200	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) FindByID(ctx *gin.Context) {
	id := ctx.Param("id")
	orgID := ctx.GetString("orgId")

	entity, err := ic.service.FindByID(ctx, id, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to fetch proxy", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	if entity == nil {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Proxy not found"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", entity))
}

// @Router		/proxies/{id} [put]
// @Summary		Update proxy
// @Tags			Proxies
// @Produce		json
// @Accept		json
// @Security BearerAuth
// @Param       id   path      string  true  "Proxy ID"
// @Param       body body     CreateUpdateDto  true  "Proxy object"
// @Success		200	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) UpdateFull(ctx *gin.Context) {
	id := ctx.Param("id")

	var entity CreateUpdateDto
	if err := ctx.ShouldBindJSON(&entity); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
		return
	}

	if err := utils.Validate.Struct(entity); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	orgID := ctx.GetString("orgId")
	entity.OrgID = orgID

	updated, err := ic.service.UpdateFull(ctx, id, &entity, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to update proxy", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("proxy updated successfully", updated))
}

// @Router		/proxies/{id} [patch]
// @Summary		Update proxy
// @Tags			Proxies
// @Produce		json
// @Accept		json
// @Security BearerAuth
// @Param       id   path      string  true  "Proxy ID"
// @Param       body body     PartialUpdateDto  true  "Proxy object"
// @Success		200	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) UpdatePartial(ctx *gin.Context) {
	id := ctx.Param("id")

	var entity PartialUpdateDto
	if err := ctx.ShouldBindJSON(&entity); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
		return
	}

	updated, err := ic.service.UpdatePartial(ctx, id, &entity, ctx.GetString("orgId"))
	if err != nil {
		ic.logger.Errorw("Failed to update proxy", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("proxy updated successfully", updated))
}

// @Router		/proxies/{id} [delete]
// @Summary		Delete proxy
// @Tags			Proxies
// @Produce		json
// @Security BearerAuth
// @Param       id   path      string  true  "Proxy ID"
// @Success		200	{object}	utils.ApiResponse[any]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) Delete(ctx *gin.Context) {
	id := ctx.Param("id")

	err := ic.service.Delete(ctx, id, ctx.GetString("orgId"))
	if err != nil {
		ic.logger.Errorw("Failed to delete proxy", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse[any]("Proxy deleted successfully", nil))
}
