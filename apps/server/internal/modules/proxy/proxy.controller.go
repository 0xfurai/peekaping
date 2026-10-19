package proxy

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
// @Param     limit query    int     false  "Items per page" default(10) minimum(1) maximum(1000)
// @Success		200	{object}	utils.ApiResponse[[]Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) FindAll(w http.ResponseWriter, r *http.Request) {
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

	entities, err := ic.service.FindAll(ctx, page, limit, q)
	if err != nil {
		ic.logger.Errorw("Failed to fetch proxies", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("success", entities))
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
func (ic *Controller) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var entity *CreateUpdateDto
	if err := httpx.DecodeJSON(r, &entity); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := utils.Validate.Struct(entity); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	created, err := ic.service.Create(ctx, entity)
	if err != nil {
		ic.logger.Errorw("Failed to create proxy", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	httpx.JSON(w, http.StatusCreated, utils.NewSuccessResponse("Proxy created successfully", created))
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
func (ic *Controller) FindByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	entity, err := ic.service.FindByID(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to fetch proxy", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	if entity == nil {
		httpx.JSON(w, http.StatusNotFound, utils.NewFailResponse("Proxy not found"))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("success", entity))
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
func (ic *Controller) UpdateFull(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	var entity CreateUpdateDto
	if err := httpx.DecodeJSON(r, &entity); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
		return
	}

	if err := utils.Validate.Struct(entity); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if _, ok := utils.FindOr404(ctx, w, ic.logger, "Proxy", ic.service.FindByID, id); !ok {
		return
	}

	updated, err := ic.service.UpdateFull(ctx, id, &entity)
	if err != nil {
		ic.logger.Errorw("Failed to update proxy", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("proxy updated successfully", updated))
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
func (ic *Controller) UpdatePartial(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	var entity PartialUpdateDto
	if err := httpx.DecodeJSON(r, &entity); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
		return
	}

	if err := utils.Validate.Struct(entity); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if _, ok := utils.FindOr404(ctx, w, ic.logger, "Proxy", ic.service.FindByID, id); !ok {
		return
	}

	updated, err := ic.service.UpdatePartial(ctx, id, &entity)
	if err != nil {
		ic.logger.Errorw("Failed to update proxy", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("proxy updated successfully", updated))
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
func (ic *Controller) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	if _, ok := utils.FindOr404(ctx, w, ic.logger, "Proxy", ic.service.FindByID, id); !ok {
		return
	}

	err := ic.service.Delete(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to delete proxy", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse[any]("Proxy deleted successfully", nil))
}
