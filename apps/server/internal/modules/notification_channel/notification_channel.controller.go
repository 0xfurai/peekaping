package notification_channel

import (
	"context"
	"errors"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel/providers"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacehttp"
	"peekaping/internal/netguard"
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
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
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

	response, err := ic.service.FindAll(ctx, workspaceID, page, limit, q)
	if err != nil {
		ic.logger.Errorw("Failed to fetch notifications", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("success", redactModels(ic.providers, response)))
}
func (ic *Controller) Create(ctx context.Context, input *CreateInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
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

	if _, err := ic.validateConfig(ctx, workspaceID, notification_channel.Type, notification_channel.Config); err != nil {
		return nil, err
	}

	createdNotification, err := ic.service.Create(ctx, workspaceID, notification_channel)
	if err != nil {
		ic.logger.Errorw("Failed to create notification", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("Notification created successfully", redactModel(ic.providers, createdNotification)))
}
func (ic *Controller) FindByID(ctx context.Context, input *FindByIDInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	notification, err := ic.service.FindByID(ctx, workspaceID, id)
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
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	notification, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := validation.Validate.Struct(notification); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	current, lookupErr := utils.RequireRecord(ctx, ic.logger, "Notification channel", findIn(ic.service, workspaceID), id)
	if lookupErr != nil {
		return nil, lookupErr
	}

	// A secret sent back as the placeholder keeps its saved value
	config, _, resolveErr := ic.resolveConfig(notification.Type, notification.Config, current)
	if resolveErr != nil {
		return nil, resolveErr
	}
	notification.Config = config

	if _, err := ic.validateConfig(ctx, workspaceID, notification.Type, notification.Config); err != nil {
		return nil, err
	}

	updatedNotification, err := ic.service.UpdateFull(ctx, workspaceID, id, &notification)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, notFoundResponse()
		}
		ic.logger.Errorw("Failed to update notification", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("notification updated successfully", redactModel(ic.providers, updatedNotification)))
}
func (ic *Controller) UpdatePartial(ctx context.Context, input *UpdatePartialInput) (*httpx.Output[utils.ApiResponse[*Model]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
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

	current, lookupErr := utils.RequireRecord(ctx, ic.logger, "Notification channel", findIn(ic.service, workspaceID), id)
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

		if _, err := ic.validateConfig(ctx, workspaceID, channelType, config); err != nil {
			return nil, err
		}
	}

	updatedNotification, err := ic.service.UpdatePartial(ctx, workspaceID, id, &notification)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, notFoundResponse()
		}
		ic.logger.Errorw("Failed to update notification", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("notification updated successfully", redactModel(ic.providers, updatedNotification)))
}
func (ic *Controller) Delete(ctx context.Context, input *DeleteInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	r := input.Request
	id := r.PathValue("id")

	if _, err := utils.RequireRecord(ctx, ic.logger, "Notification channel", findIn(ic.service, workspaceID), id); err != nil {
		return nil, err
	}

	err = ic.service.Delete(ctx, workspaceID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, notFoundResponse()
		}
		ic.logger.Errorw("Failed to delete notification", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Notification deleted successfully", nil))
}
func (ic *Controller) Test(ctx context.Context, input *TestInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
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
		record, err := utils.RequireRecord(ctx, ic.logger, "Notification channel", findIn(ic.service, workspaceID), notificationChannel.ID)
		if err != nil {
			return nil, err
		}
		saved = record
	}
	config, resolvedSecrets, resolveErr := ic.resolveConfig(notificationChannel.Type, notificationChannel.Config, saved)
	if resolveErr != nil {
		return nil, resolveErr
	}

	integration, lookupErr := ic.validateConfig(ctx, workspaceID, notificationChannel.Type, config)
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
	sendCtx, cancel := context.WithTimeout(providers.WithWorkspace(ctx, workspaceID), sendTimeout)
	defer cancel()
	// An HTTP client's errors hold the request URL, and for a channel the URL holds a credential
	err = providers.RedactURLErrors(integration.Send(sendCtx, config, testMessage, testMonitor, testHeartbeat))
	if errors.Is(err, netguard.ErrBlocked) {
		// The channel's address resolves to one the install blocks, which only the caller can change
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Failed to send test notification: "+maskSecrets(err.Error(), resolvedSecrets)))
	}
	if errors.Is(err, providers.ErrRecipientNotVerified) {
		// The caller can fix it, as with a config the provider refuses: their recipient has to verify
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Failed to send test notification: "+err.Error()))
	}
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

// validateConfig looks up the provider for channelType and checks config with it, for a channel
// of the workspace. On failure it returns the existing 400 response.
func (ic *Controller) validateConfig(ctx context.Context, workspaceID workspace.ID, channelType, config string) (NotificationChannelProvider, error) {
	integration, ok := ic.providers.Get(channelType)
	if !ok {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Unsupported notification type"))
	}
	if err := integration.Validate(config); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid config: "+err.Error()))
	}
	// With BLOCK_PRIVATE_TARGETS on, a URL or host that is a blocked address can't be saved or tested.
	// Both answer 400 with the code a client reads, as a monitor does.
	if err := ic.providers.CheckTargets(channelType, config); err != nil {
		var blocked *netguard.TargetError
		if errors.As(err, &blocked) {
			return nil, utils.TargetNotAllowed(blocked)
		}
		return nil, err
	}
	if scoped, ok := integration.(WorkspaceValidator); ok {
		if err := scoped.ValidateInWorkspace(providers.WithWorkspace(ctx, workspaceID), config); err != nil {
			return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid config: "+err.Error()))
		}
	}
	return integration, nil
}

// findIn is the lookup of a channel in the workspace of the request: a channel of another workspace
// is not found, as one that doesn't exist is
func findIn(service Service, workspaceID workspace.ID) func(context.Context, string) (*Model, error) {
	return func(ctx context.Context, id string) (*Model, error) {
		return service.FindByID(ctx, workspaceID, id)
	}
}

// notFoundResponse is the answer for a channel that doesn't exist and for one that belongs to
// another workspace, which a caller can't tell apart
func notFoundResponse() error {
	return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Notification channel not found"))
}
