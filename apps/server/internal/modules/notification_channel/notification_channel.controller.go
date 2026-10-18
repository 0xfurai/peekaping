package notification_channel

import (
	"net/http"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/utils"

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
// @Param     limit query    int     false  "Items per page" default(10) minimum(1) maximum(1000)
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
	if err != nil || limit < 1 || limit > utils.MaxPageSize {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid limit parameter"))
		return
	}

	q := ctx.Query("q")

	response, err := ic.service.FindAll(ctx, page, limit, q)
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

	if _, ok := validateConfig(ctx, notification_channel.Type, notification_channel.Config); !ok {
		return
	}

	createdNotification, err := ic.service.Create(ctx, notification_channel)
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

	notification, err := ic.service.FindByID(ctx, id)
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

	if _, ok := validateConfig(ctx, notification.Type, notification.Config); !ok {
		return
	}

	updatedNotification, err := ic.service.UpdateFull(ctx, id, &notification)
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

	// Validate the type and config the channel will have after the update; omitted fields keep their saved values
	if notification.Type != nil || notification.Config != nil {
		existing, err := ic.service.FindByID(ctx, id)
		if err != nil {
			ic.logger.Errorw("Failed to fetch notification", "error", err)
			ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
			return
		}
		if existing == nil {
			ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Notification not found"))
			return
		}

		channelType := existing.Type
		if notification.Type != nil {
			channelType = *notification.Type
		}
		config := ""
		if existing.Config != nil {
			config = *existing.Config
		}
		if notification.Config != nil {
			config = *notification.Config
		}

		if _, ok := validateConfig(ctx, channelType, config); !ok {
			return
		}
	}

	updatedNotification, err := ic.service.UpdatePartial(ctx, id, &notification)
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

	err := ic.service.Delete(ctx, id)
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

	integration, ok := validateConfig(ctx, notificationChannel.Type, notificationChannel.Config)
	if !ok {
		return
	}

	// Create a test message and monitor for the notification
	testMessage := "This is a test notification from Peekaping"
	testMonitor := &monitor.Model{
		Name: "Test Monitor",
		Type: "http",
	}
	testHeartbeat := &heartbeat.Model{
		Status: shared.MonitorStatusDown,
		Msg:    testMessage,
	}

	// Send the test notification
	err := integration.Send(ctx, notificationChannel.Config, testMessage, testMonitor, testHeartbeat)
	if err != nil {
		ic.logger.Errorw("Failed to send test notification", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Failed to send test notification: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse[any]("Test notification sent successfully", nil))
}

// validateConfig looks up the provider for channelType and checks config with it.
// On failure it writes the 400 response and returns false.
func validateConfig(ctx *gin.Context, channelType, config string) (NotificationChannelProvider, bool) {
	integration, ok := GetNotificationChannelProvider(channelType)
	if !ok {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Unsupported notification type"))
		return nil, false
	}
	if err := integration.Validate(config); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid config: "+err.Error()))
		return nil, false
	}
	return integration, true
}
