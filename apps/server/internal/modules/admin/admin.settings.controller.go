package admin

import (
	"context"
	"errors"
	"net/http"

	"peekaping/internal/httpx"
	"peekaping/internal/installsettings"
	"peekaping/internal/mailer"
	"peekaping/internal/utils"
)

func (c *Controller) GetInstallSettings(ctx context.Context, _ *GetInstallSettingsInput) (*httpx.Output[utils.ApiResponse[*InstallSettingsResponse]], error) {
	view, err := c.settings.Describe(ctx)
	if err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Install settings retrieved successfully", installSettingsResponse(view)))
}

func (c *Controller) UpdateInstallSettings(ctx context.Context, input *UpdateInstallSettingsInput) (*httpx.Output[utils.ApiResponse[*InstallSettingsResponse]], error) {
	actorID, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	body, err := input.Decode()
	if err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}
	view, err := c.settings.Update(ctx, actorID, body.change())
	if err != nil {
		var refused *installsettings.Error
		if errors.As(err, &refused) {
			status := http.StatusBadRequest
			if refused.Code == installsettings.CodeLocked {
				status = http.StatusConflict
			}
			return nil, httpx.Fail(status, SettingErrorResponse{Message: refused.Error(), Error: SettingErrorBody{Code: refused.Code, Field: refused.Field}})
		}
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Install settings updated successfully", installSettingsResponse(view)))
}

func (c *Controller) SendTestEmail(ctx context.Context, input *SendTestEmailInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	actorID, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	body, err := input.Decode()
	if err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}
	switch err := c.settings.SendTestEmail(ctx, actorID, body.To); {
	case err == nil:
		return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Test email sent", nil))
	case errors.Is(err, ErrInvalidRecipient):
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("to must be an email address"))
	case errors.Is(err, ErrNoSender):
		return nil, httpx.Fail(http.StatusBadRequest, SettingErrorResponse{Message: "The install has no sender for account emails", Error: SettingErrorBody{Code: "NO_SENDER"}})
	default:
		// The server's text is left out: it can echo an address (see mailer.Describe)
		return nil, httpx.Fail(http.StatusBadGateway, utils.NewFailResponse("The test email was not sent: "+mailer.Describe(err)))
	}
}
