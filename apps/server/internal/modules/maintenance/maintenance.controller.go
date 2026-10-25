package maintenance

import (
	"context"
	"fmt"
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
	strategy := r.URL.Query().Get("strategy")

	entities, err := ic.service.FindAll(ctx, page, limit, q, strategy)
	if err != nil {
		ic.logger.Errorw("Failed to fetch maintenances", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", entities))
}
func (ic *Controller) Create(ctx context.Context, input *CreateInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	entity, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}
	entity.ApplyCreateDefaults()

	if err := validation.Validate.Struct(entity); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	created, err := ic.service.Create(ctx, entity.toModel(), entity.MonitorIds)
	if err != nil {
		ic.logger.Errorw("Failed to create maintenance", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("Maintenance created successfully", created))
}
func (ic *Controller) FindByID(ctx context.Context, input *FindByIDInput) (*httpx.Output[utils.ApiResponse[*MaintenanceResponseDto]], error) {
	r := input.Request
	id := r.PathValue("id")

	entity, err := ic.service.FindByID(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to fetch maintenance", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	if entity == nil {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Maintenance window not found"))
	}

	// Get monitor IDs
	monitorIds, err := ic.service.GetMonitors(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to fetch monitor IDs", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
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

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", response))
}
func (ic *Controller) UpdateFull(ctx context.Context, input *UpdateFullInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	id := r.PathValue("id")

	entity, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := validation.Validate.Struct(entity); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if _, err := utils.RequireRecord(ctx, ic.logger, "Maintenance window", ic.service.FindByID, id); err != nil {
		return nil, err
	}

	updated, err := ic.service.UpdateFull(ctx, id, entity.toModel(), entity.MonitorIds)
	if err != nil {
		ic.logger.Errorw("Failed to update maintenance", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("maintenance updated successfully", updated))
}
func (ic *Controller) UpdatePartial(ctx context.Context, input *UpdatePartialInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	id := r.PathValue("id")

	entity, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := validation.Validate.Struct(entity); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if _, err := utils.RequireRecord(ctx, ic.logger, "Maintenance window", ic.service.FindByID, id); err != nil {
		return nil, err
	}

	updated, err := ic.service.UpdatePartial(ctx, id, entity.toModel(), entity.MonitorIds)
	if err != nil {
		ic.logger.Errorw("Failed to update maintenance", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("maintenance updated successfully", updated))
}
func (ic *Controller) Delete(ctx context.Context, input *DeleteInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	r := input.Request
	id := r.PathValue("id")

	if _, err := utils.RequireRecord(ctx, ic.logger, "Maintenance window", ic.service.FindByID, id); err != nil {
		return nil, err
	}

	err := ic.service.Delete(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to delete maintenance", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Maintenance deleted successfully", nil))
}
func (ic *Controller) Pause(ctx context.Context, input *PauseInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	fmt.Println("Pausing maintenance")
	id := r.PathValue("id")
	if _, err := utils.RequireRecord(ctx, ic.logger, "Maintenance window", ic.service.FindByID, id); err != nil {
		return nil, err
	}
	updated, err := ic.service.SetActive(ctx, id, false)
	if err != nil {
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Failed to pause maintenance"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Paused", updated))
}
func (ic *Controller) Resume(ctx context.Context, input *ResumeInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	id := r.PathValue("id")
	if _, err := utils.RequireRecord(ctx, ic.logger, "Maintenance window", ic.service.FindByID, id); err != nil {
		return nil, err
	}
	updated, err := ic.service.SetActive(ctx, id, true)
	if err != nil {
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Failed to resume maintenance"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Resumed", updated))
}
