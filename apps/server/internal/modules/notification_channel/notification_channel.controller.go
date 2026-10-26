package notification_channel

import (
	"context"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel/providers"
	"peekaping/internal/modules/shared"
	"peekaping/internal/utils"
	"peekaping/internal/validation"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

type Controller struct {
	service   Service
	providers *ProviderRegistry
	logger    *zap.SugaredLogger
}

func NewController(
	service Service,
	providers *ProviderRegistry,
	logger *zap.SugaredLogger,
) *Controller {
	return &Controller{
		service,
		providers,
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

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", redactModels(ic.providers, response)))
}
func (ic *Controller) Create(ctx context.Context, input *CreateInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	notification_channel, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		ic.logger.Errorw("Invalid request body", "error", err)
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}
	notification_channel.ApplyCreateDefaults()

	if err := validation.Validate.Struct(notification_channel); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}

	// A new channel has no saved secrets for a placeholder to stand for
	config, _, resolveErr := ic.resolveConfig(notification_channel.Type, notification_channel.Config, nil)
	if resolveErr != nil {
		return nil, resolveErr
	}
	notification_channel.Config = config

	if _, err := ic.validateConfig(notification_channel.Type, notification_channel.Config); err != nil {
		return nil, err
	}

	createdNotification, err := ic.service.Create(ctx, notification_channel)
	if err != nil {
		ic.logger.Errorw("Failed to create notification", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("Notification created successfully", redactModel(ic.providers, createdNotification)))
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

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", redactModel(ic.providers, notification)))
}
func (ic *Controller) UpdateFull(ctx context.Context, input *UpdateFullInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	id := r.PathValue("id")

	notification, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := validation.Validate.Struct(notification); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	current, lookupErr := utils.RequireRecord(ctx, ic.logger, "Notification channel", ic.service.FindByID, id)
	if lookupErr != nil {
		return nil, lookupErr
	}

	// A secret sent back as the placeholder keeps its saved value
	config, _, resolveErr := ic.resolveConfig(notification.Type, notification.Config, current)
	if resolveErr != nil {
		return nil, resolveErr
	}
	notification.Config = config

	if _, err := ic.validateConfig(notification.Type, notification.Config); err != nil {
		return nil, err
	}

	updatedNotification, err := ic.service.UpdateFull(ctx, id, &notification)
	if err != nil {
		ic.logger.Errorw("Failed to update notification", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("notification updated successfully", redactModel(ic.providers, updatedNotification)))
}
func (ic *Controller) UpdatePartial(ctx context.Context, input *UpdatePartialInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	r := input.Request
	id := r.PathValue("id")

	notification, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	// validate
	if err := validation.Validate.Struct(notification); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	current, lookupErr := utils.RequireRecord(ctx, ic.logger, "Notification channel", ic.service.FindByID, id)
	if lookupErr != nil {
		return nil, lookupErr
	}

	channelType := current.Type
	if notification.Type != nil {
		channelType = *notification.Type
	}

	// The saved config would be read by the new type's provider, which doesn't know the old
	// type's secret keys and so wouldn't hide them. The new type brings its own config.
	if channelType != current.Type && notification.Config == nil {
		if _, ok := ic.providers.Get(channelType); !ok {
			return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Unsupported notification type"))
		}
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid config: send the config when changing the type"))
	}

	// A secret sent back as the placeholder keeps its saved value
	if notification.Config != nil {
		resolved, _, resolveErr := ic.resolveConfig(channelType, *notification.Config, current)
		if resolveErr != nil {
			return nil, resolveErr
		}
		notification.Config = &resolved
	}

	// Validate the type and config the channel will have after the update; omitted fields keep their saved values
	if notification.Type != nil || notification.Config != nil {
		config := configOf(current)
		if notification.Config != nil {
			config = *notification.Config
		}

		if _, err := ic.validateConfig(channelType, config); err != nil {
			return nil, err
		}
	}

	updatedNotification, err := ic.service.UpdatePartial(ctx, id, &notification)
	if err != nil {
		ic.logger.Errorw("Failed to update notification", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("notification updated successfully", redactModel(ic.providers, updatedNotification)))
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

	if err := validation.Validate.Struct(notificationChannel); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}

	// A secret sent as the placeholder is the one the saved channel has, if the request says which
	// channel it edits
	var saved *Model
	if notificationChannel.ID != "" {
		record, err := utils.RequireRecord(ctx, ic.logger, "Notification channel", ic.service.FindByID, notificationChannel.ID)
		if err != nil {
			return nil, err
		}
		saved = record
	}
	config, resolvedSecrets, resolveErr := ic.resolveConfig(notificationChannel.Type, notificationChannel.Config, saved)
	if resolveErr != nil {
		return nil, resolveErr
	}

	integration, lookupErr := ic.validateConfig(notificationChannel.Type, config)
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
	// An HTTP client's errors hold the request URL, and for a channel the URL holds a credential
	err := providers.RedactURLErrors(integration.Send(sendCtx, config, testMessage, testMonitor, testHeartbeat))
	if err != nil {
		// A failed send repeats what it was sent to, which has the secrets in it. The ones taken from
		// the saved channel are not the caller's to read back.
		failure := maskSecrets(err.Error(), resolvedSecrets)
		ic.logger.Errorw("Failed to send test notification", "error", failure)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Failed to send test notification: "+failure))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Test notification sent successfully", nil))
}

// configOf returns the config a channel is saved with, which is empty when it has none
func configOf(channel *Model) string {
	if channel.Config == nil {
		return ""
	}
	return *channel.Config
}

// resolveConfig replaces each secret that config sends as the placeholder with the one the saved
// channel holds. saved is the channel the request edits, or nil when there is none. A placeholder
// that can't be resolved is a 400: that is, when there is no saved channel, when it has another
// type, or when the config changed where the channel sends. It also returns the saved secrets it
// put in.
//
// A config that names a key twice, in any letter case, is a 400 before anything is resolved. A
// type without a provider is left for validateConfig to refuse.
func (ic *Controller) resolveConfig(channelType, config string, saved *Model) (string, []string, error) {
	if key, earlier, found := duplicateKey(config); found {
		return "", nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(
			"Invalid config: duplicate key "+strconv.Quote(key)+" (the config already has "+strconv.Quote(earlier)+")"))
	}
	provider, ok := ic.providers.Get(channelType)
	if !ok {
		return config, nil, nil
	}
	var savedConfig *string
	if saved != nil && saved.Type == channelType {
		savedConfigValue := configOf(saved)
		savedConfig = &savedConfigValue
	}
	resolved, unresolved, used := resolveSecrets(provider, config, savedConfig)
	if len(unresolved) > 0 {
		return "", nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(
			"Invalid config: re-enter the saved secrets ("+strings.Join(unresolved, ", ")+") when changing the type or where the channel sends"))
	}
	return resolved, used, nil
}

// validateConfig looks up the provider for channelType and checks config with it.
// On failure it returns the existing 400 response.
func (ic *Controller) validateConfig(channelType, config string) (NotificationChannelProvider, error) {
	integration, ok := ic.providers.Get(channelType)
	if !ok {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Unsupported notification type"))
	}
	if err := integration.Validate(config); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid config: "+err.Error()))
	}
	return integration, nil
}
