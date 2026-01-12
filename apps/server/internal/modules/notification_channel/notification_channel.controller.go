package notification_channel

import (
	"net/http"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/monitor"
	"vigi/internal/modules/shared"
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

// @Router		/notification-channels [get]
// @Summary		Get notification channels
// @Tags			Notification channels
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
func (ic *Controller) FindAll(ctx *gin.Context) {
	// Extract query parameters for pagination and search
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

	response, err := ic.service.FindAll(ctx, page, limit, q, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to fetch notifications", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", response))
}

// @Router		/notification-channels [post]
// @Summary		Create notification channel
// @Tags			Notification channels
// @Produce		json
// @Accept		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     body body   CreateUpdateDto  true  "Notification object"
// @Success		201	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) Create(ctx *gin.Context) {
	var notification_channel *CreateUpdateDto
	if err := ctx.ShouldBindJSON(&notification_channel); err != nil {
		ic.logger.Errorw("Invalid request body", "error", err)
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
		return
	}

	if err := utils.Validate.Struct(notification_channel); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
		return
	}

	integration, ok := GetNotificationChannelProvider(notification_channel.Type)
	if !ok {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Unsupported notification type"))
		return
	}
	err := integration.Validate(notification_channel.Config)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid config: "+err.Error()))
		return
	}

	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	createdNotification, err := ic.service.Create(ctx, notification_channel, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to create notification", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusCreated, utils.NewSuccessResponse("Notification created successfully", createdNotification))
}

// @Router		/notification-channels/{id} [get]
// @Summary		Get notification channel by ID
// @Tags			Notification channels
// @Produce		json
// @Security BearerAuth
// @Param       id   path      string  true  "Notification ID"
// @Success		200	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) FindByID(ctx *gin.Context) {
	id := ctx.Param("id")

	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	notification, err := ic.service.FindByID(ctx, id, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to fetch notification", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	if notification == nil {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Notification not found"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", notification))
}

// @Router		/notification-channels/{id} [put]
// @Summary		Update notification channel
// @Tags			Notification channels
// @Produce		json
// @Accept		json
// @Security BearerAuth
// @Param       id   path      string  true  "Notification ID"
// @Param       notification body     CreateUpdateDto  true  "Notification object"
// @Success		200	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) UpdateFull(ctx *gin.Context) {
	id := ctx.Param("id")

	var notification CreateUpdateDto
	if err := ctx.ShouldBindJSON(&notification); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := utils.Validate.Struct(notification); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	updatedNotification, err := ic.service.UpdateFull(ctx, id, &notification, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to update notification", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("notification updated successfully", updatedNotification))
}

// @Router		/notification-channels/{id} [patch]
// @Summary		Update notification channel
// @Tags			Notification channels
// @Produce		json
// @Accept		json
// @Security BearerAuth
// @Param       id   path      string  true  "Notification ID"
// @Param       notification body     PartialUpdateDto  true  "Notification object"
// @Success		200	{object}	utils.ApiResponse[Model]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) UpdatePartial(ctx *gin.Context) {
	id := ctx.Param("id")

	var notification PartialUpdateDto
	if err := ctx.ShouldBindJSON(&notification); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// validate
	if err := utils.Validate.Struct(notification); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	updatedNotification, err := ic.service.UpdatePartial(ctx, id, &notification, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to update notification", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("notification updated successfully", updatedNotification))
}

// @Router		/notification-channels/{id} [delete]
// @Summary		Delete notification channel
// @Tags			Notification channels
// @Produce		json
// @Security BearerAuth
// @Param       id   path      string  true  "Notification ID"
// @Success		200	{object}	utils.ApiResponse[any]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) Delete(ctx *gin.Context) {
	id := ctx.Param("id")

	// Extract orgID from context
	orgID := ctx.GetString("orgId")

	err := ic.service.Delete(ctx, id, orgID)
	if err != nil {
		ic.logger.Errorw("Failed to delete notification", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse[any]("Notification deleted successfully", nil))
}

// @Router		/notification-channels/test [post]
// @Summary		Test notification channel
// @Tags			Notification channels
// @Produce		json
// @Accept		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     body body   CreateUpdateDto  true  "Notification object"
// @Success		200	{object}	utils.ApiResponse[any]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (ic *Controller) Test(ctx *gin.Context) {
	var notificationChannel *CreateUpdateDto
	if err := ctx.ShouldBindJSON(&notificationChannel); err != nil {
		ic.logger.Errorw("Invalid request body", "error", err)
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
		return
	}

	if err := utils.Validate.Struct(notificationChannel); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
		return
	}

	integration, ok := GetNotificationChannelProvider(notificationChannel.Type)
	if !ok {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Unsupported notification type"))
		return
	}
	err := integration.Validate(notificationChannel.Config)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid config: "+err.Error()))
		return
	}

	// Create a test message and monitor for the notification
	testMessage := "This is a test notification from Vigi"
	testMonitor := &monitor.Model{
		Name: "Test Monitor",
		Type: "http",
	}
	testHeartbeat := &heartbeat.Model{
		Status: shared.MonitorStatusDown,
		Msg:    testMessage,
	}

	// Send the test notification
	err = integration.Send(ctx, notificationChannel.Config, testMessage, testMonitor, testHeartbeat)
	if err != nil {
		ic.logger.Errorw("Failed to send test notification", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Failed to send test notification: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse[any]("Test notification sent successfully", nil))
}
