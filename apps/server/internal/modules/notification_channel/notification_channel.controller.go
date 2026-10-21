package notification_channel

import (
	"context"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
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
func (ic *Controller) FindAll(ctx context.Context, input *FindAllInput) (*httpx.Output[utils.ApiResponse[[]*Model]], error) {
	r := input.Request
	// Extract query parameters for pagination and search
	page, err := utils.GetQueryInt(r, "page", 0)
	if err != nil || page < 0 {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid page parameter"))
	}

	limit, err := utils.GetQueryInt(r, "limit", 10)
	if err != nil || limit < 1 || limit > utils.MaxPageSize {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid limit parameter"))
	}

	q := r.URL.Query().Get("q")

	response, err := ic.service.FindAll(ctx, page, limit, q)
	if err != nil {
		ic.logger.Errorw("Failed to fetch notifications", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", response))
}
func (ic *Controller) Create(ctx context.Context, input *CreateInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	notification_channel, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		ic.logger.Errorw("Invalid request body", "error", err)
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}
	notification_channel.ApplyCreateDefaults()

	if err := utils.Validate.Struct(notification_channel); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}

	if _, err := validateConfig(notification_channel.Type, notification_channel.Config); err != nil {
		return nil, err
	}

	createdNotification, err := ic.service.Create(ctx, notification_channel)
	if err != nil {
		ic.logger.Errorw("Failed to create notification", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("Notification created successfully", createdNotification))
}
func (ic *Controller) FindByID(ctx context.Context, input *FindByIDInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	id := r.PathValue("id")

	notification, err := ic.service.FindByID(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to fetch notification", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	if notification == nil {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Notification channel not found"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", notification))
}
func (ic *Controller) UpdateFull(ctx context.Context, input *UpdateFullInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	id := r.PathValue("id")

	notification, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := utils.Validate.Struct(notification); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if _, err := validateConfig(notification.Type, notification.Config); err != nil {
		return nil, err
	}

	if _, err := utils.RequireRecord(ctx, ic.logger, "Notification channel", ic.service.FindByID, id); err != nil {
		return nil, err
	}

	updatedNotification, err := ic.service.UpdateFull(ctx, id, &notification)
	if err != nil {
		ic.logger.Errorw("Failed to update notification", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("notification updated successfully", updatedNotification))
}
func (ic *Controller) UpdatePartial(ctx context.Context, input *UpdatePartialInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	id := r.PathValue("id")

	notification, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	// validate
	if err := utils.Validate.Struct(notification); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	current, lookupErr := utils.RequireRecord(ctx, ic.logger, "Notification channel", ic.service.FindByID, id)
	if lookupErr != nil {
		return nil, lookupErr
	}

	// Validate the type and config the channel will have after the update; omitted fields keep their saved values
	if notification.Type != nil || notification.Config != nil {
		channelType := current.Type
		if notification.Type != nil {
			channelType = *notification.Type
		}
		config := ""
		if current.Config != nil {
			config = *current.Config
		}
		if notification.Config != nil {
			config = *notification.Config
		}

		if _, err := validateConfig(channelType, config); err != nil {
			return nil, err
		}
	}

	updatedNotification, err := ic.service.UpdatePartial(ctx, id, &notification)
	if err != nil {
		ic.logger.Errorw("Failed to update notification", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("notification updated successfully", updatedNotification))
}
func (ic *Controller) Delete(ctx context.Context, input *DeleteInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	r := input.Request
	id := r.PathValue("id")

	if _, err := utils.RequireRecord(ctx, ic.logger, "Notification channel", ic.service.FindByID, id); err != nil {
		return nil, err
	}

	err := ic.service.Delete(ctx, id)
	if err != nil {
		ic.logger.Errorw("Failed to delete notification", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Notification deleted successfully", nil))
}
func (ic *Controller) Test(ctx context.Context, input *TestInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	notificationChannel, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		ic.logger.Errorw("Invalid request body", "error", err)
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}

	if err := utils.Validate.Struct(notificationChannel); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}

	integration, lookupErr := validateConfig(notificationChannel.Type, notificationChannel.Config)
	if lookupErr != nil {
		return nil, lookupErr
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

	// Send the test notification, with the deadline the listener gives each send
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	err := integration.Send(sendCtx, notificationChannel.Config, testMessage, testMonitor, testHeartbeat)
	if err != nil {
		ic.logger.Errorw("Failed to send test notification", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Failed to send test notification: "+err.Error()))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Test notification sent successfully", nil))
}

// validateConfig looks up the provider for channelType and checks config with it.
// On failure it returns the existing 400 response.
func validateConfig(channelType, config string) (NotificationChannelProvider, error) {
	integration, ok := GetNotificationChannelProvider(channelType)
	if !ok {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Unsupported notification type"))
	}
	if err := integration.Validate(config); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid config: "+err.Error()))
	}
	return integration, nil
}
