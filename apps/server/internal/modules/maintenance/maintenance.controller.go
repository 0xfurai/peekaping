package maintenance

import (
	"fmt"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/utils"

	"go.mongodb.org/mongo-driver/bson"
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

// @Router		/maintenances [get]
// @Summary		Get maintenances
// @Tags			Maintenances
// @Produce		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     q    query     string  false  "Search query"
// @Param     strategy query string  false  "Filter by strategy"
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
	strategy := r.URL.Query().Get("strategy")

	filter := bson.M{}
	if q != "" {
		filter["$or"] = bson.A{
			bson.M{"title": bson.M{"$regex": q, "$options": "i"}},
			bson.M{"description": bson.M{"$regex": q, "$options": "i"}},
		}
	}

	entities, err := ic.service.FindAll(ctx, page, limit, q, strategy)
	if err != nil {
		ic.logger.Errorw("Failed to fetch maintenances", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("success", entities))
}

// @Router		/maintenances [post]
// @Summary		Create maintenance
// @Tags			Maintenances
// @Produce		json
// @Accept		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     body body   CreateUpdateDto  true  "Maintenance object"
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

	created, err := ic.service.Create(ctx, entity.toModel(), entity.MonitorIds)
	if err != nil {
		ic.logger.Errorw("Failed to create maintenance", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	httpx.JSON(w, http.StatusCreated, utils.NewSuccessResponse("Maintenance created successfully", created))
}

// @Router		/maintenances/{id} [get]
// @Summary		Get maintenance by ID
// @Tags			Maintenances
// @Produce		json
// @Security BearerAuth
// @Param       id   path      string  true  "Maintenance ID"
// @Success		200	{object}	utils.ApiResponse[MaintenanceResponseDto]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) FindByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	entity, err := ic.service.FindByID(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to fetch maintenance", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	if entity == nil {
		httpx.JSON(w, http.StatusNotFound, utils.NewFailResponse("Maintenance window not found"))
		return
	}

	// Get monitor IDs
	monitorIds, err := ic.service.GetMonitors(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to fetch monitor IDs", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	response := &MaintenanceResponseDto{
		ID:            entity.ID,
		Title:         entity.Title,
		Description:   entity.Description,
		Active:        entity.Active,
		Strategy:      entity.Strategy,
		StartDateTime: entity.StartDateTime,
		EndDateTime:   entity.EndDateTime,
		StartTime:     entity.StartTime,
		EndTime:       entity.EndTime,
		Weekdays:      entity.Weekdays,
		DaysOfMonth:   entity.DaysOfMonth,
		IntervalDay:   entity.IntervalDay,
		Cron:          entity.Cron,
		Timezone:      entity.Timezone,
		Duration:      entity.Duration,
		CreatedAt:     entity.CreatedAt,
		UpdatedAt:     entity.UpdatedAt,
		MonitorIds:    monitorIds,
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("success", response))
}

// @Router		/maintenances/{id} [put]
// @Summary		Update maintenance
// @Tags			Maintenances
// @Produce		json
// @Accept		json
// @Security BearerAuth
// @Param       id   path      string  true  "Maintenance ID"
// @Param       body body     CreateUpdateDto  true  "Maintenance object"
// @Success		200	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) UpdateFull(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	var entity CreateUpdateDto
	if err := httpx.DecodeJSON(r, &entity); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := utils.Validate.Struct(entity); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if _, ok := utils.FindOr404(ctx, w, ic.logger, "Maintenance window", ic.service.FindByID, id); !ok {
		return
	}

	updated, err := ic.service.UpdateFull(ctx, id, entity.toModel(), entity.MonitorIds)
	if err != nil {
		ic.logger.Errorw("Failed to update maintenance", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("maintenance updated successfully", updated))
}

// @Router		/maintenances/{id} [patch]
// @Summary		Update maintenance
// @Tags			Maintenances
// @Produce		json
// @Accept		json
// @Security BearerAuth
// @Param       id   path      string  true  "Maintenance ID"
// @Param       body body     PartialUpdateDto  true  "Maintenance object"
// @Success		200	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) UpdatePartial(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	var entity PartialUpdateDto
	if err := httpx.DecodeJSON(r, &entity); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := utils.Validate.Struct(entity); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if _, ok := utils.FindOr404(ctx, w, ic.logger, "Maintenance window", ic.service.FindByID, id); !ok {
		return
	}

	updated, err := ic.service.UpdatePartial(ctx, id, entity.toModel(), entity.MonitorIds)
	if err != nil {
		ic.logger.Errorw("Failed to update maintenance", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("maintenance updated successfully", updated))
}

// @Router		/maintenances/{id} [delete]
// @Summary		Delete maintenance
// @Tags			Maintenances
// @Produce		json
// @Security BearerAuth
// @Param       id   path      string  true  "Maintenance ID"
// @Success		200	{object}	utils.ApiResponse[any]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	if _, ok := utils.FindOr404(ctx, w, ic.logger, "Maintenance window", ic.service.FindByID, id); !ok {
		return
	}

	err := ic.service.Delete(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to delete maintenance", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse[any]("Maintenance deleted successfully", nil))
}

// @Router		/maintenances/{id}/pause [patch]
// @Summary		Pause maintenance
// @Tags			Maintenances
// @Produce		json
// @Security BearerAuth
// @Param       id   path      string  true  "Maintenance ID"
// @Success		200	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) Pause(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	fmt.Println("Pausing maintenance")
	id := r.PathValue("id")
	if _, ok := utils.FindOr404(ctx, w, ic.logger, "Maintenance window", ic.service.FindByID, id); !ok {
		return
	}
	updated, err := ic.service.SetActive(ctx, id, false)
	if err != nil {
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Failed to pause maintenance"))
		return
	}
	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("Paused", updated))
}

// @Router		/maintenances/{id}/resume [patch]
// @Summary		Resume maintenance
// @Tags			Maintenances
// @Produce		json
// @Security BearerAuth
// @Param       id   path      string  true  "Maintenance ID"
// @Success		200	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) Resume(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if _, ok := utils.FindOr404(ctx, w, ic.logger, "Maintenance window", ic.service.FindByID, id); !ok {
		return
	}
	updated, err := ic.service.SetActive(ctx, id, true)
	if err != nil {
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Failed to resume maintenance"))
		return
	}
	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("Resumed", updated))
}
