package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"peekaping/internal/httpx"
	"peekaping/internal/utils"
)

// forgotPasswordMessage is the answer to every request for a reset link, whether the address has
// an account or not
const forgotPasswordMessage = "If an account exists for this email address, a link to reset the password is on its way"

// AuthConfig tells the app, before anyone signs in, whether the install sends account emails and
// who may sign up
func (c *Controller) AuthConfig(ctx context.Context, _ *AuthConfigInput) (*httpx.Output[utils.ApiResponse[*AuthConfigResponse]], error) {
	first, err := c.signup.FirstSignup(ctx)
	if err != nil {
		c.logger.Errorw("Failed to tell whether the install has a user", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	// The sign-up form is offered always in open mode, and in invite mode to the first user
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Auth config retrieved successfully", &AuthConfigResponse{
		AccountEmails:    c.accounts.Enabled(),
		SignupMode:       c.signup.Mode(),
		SignupOpen:       c.signup.Open() || first,
		FirstSignup:      first,
		TurnstileSiteKey: c.signup.TurnstileSiteKey(),
	}))
}

// clientKey is what a per-client limit counts a request under: its client address. When the address
// cannot be read (the connection has no host:port, as on a unix socket), the raw RemoteAddr is
// the key, so that such requests don't all share one bucket.
func (c *Controller) clientKey(r *http.Request) string {
	if ip := c.proxies.ClientIP(r); ip != "" {
		return ip
	}
	c.logger.Debugw("The client address could not be read, counting the request by its raw remote address", "remoteAddr", r.RemoteAddr)
	return "raw:" + r.RemoteAddr
}

func tooManyRequests() error {
	return httpx.Fail(http.StatusTooManyRequests, utils.NewFailResponse("Too many requests, try again later"))
}

// ForgotPassword sends a reset link to the account with the email, if there is one. It answers 202
// with the same body for every address, so that it does not tell a visitor which addresses have
// accounts. Only a client over its limit is told to wait (429), which depends on the client and not
// on the address. The same address asked for more than five times in an hour gets the answer and
// no email.
func (c *Controller) ForgotPassword(ctx context.Context, input *ForgotPasswordInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	dto, decodeErr := input.Decode()
	if decodeErr != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(decodeErr.Error()))
	}
	if err := c.validateWithDetails(dto); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}
	if !c.accounts.Enabled() {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse(ErrAccountEmailsOff.Error()))
	}
	if allowed, _ := c.forgotByIP.Allow(c.clientKey(input.Request)); !allowed {
		return nil, tooManyRequests()
	}
	email := strings.ToLower(strings.TrimSpace(dto.Email))
	if allowed, _ := c.forgotByEmail.Allow(email); allowed {
		c.accounts.RequestPasswordReset(email)
	} else {
		c.logger.Infow("A password reset email was not sent: the address asked too often")
	}
	return httpx.Reply(http.StatusAccepted, utils.NewSuccessResponse[any](forgotPasswordMessage, nil))
}

// ResetPassword sets a new password with the token of a reset link
func (c *Controller) ResetPassword(ctx context.Context, input *ResetPasswordInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	if allowed, _ := c.resetByIP.Allow(c.clientKey(input.Request)); !allowed {
		return nil, tooManyRequests()
	}
	dto, decodeErr := input.Decode()
	if decodeErr != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(decodeErr.Error()))
	}
	if err := c.validateWithDetails(dto); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}
	if err := c.accounts.ResetPassword(ctx, dto.Token, dto.Password); err != nil {
		if errors.Is(err, ErrInvalidLink) {
			return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("This link is invalid or has expired. Ask for a new one."))
		}
		c.logger.Errorw("Failed to reset a password", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Password reset successfully. Sign in with the new password.", nil))
}

// VerifyEmail verifies an address with the token of a verification link
func (c *Controller) VerifyEmail(ctx context.Context, input *VerifyEmailInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	if allowed, _ := c.verifyByIP.Allow(c.clientKey(input.Request)); !allowed {
		return nil, tooManyRequests()
	}
	dto, decodeErr := input.Decode()
	if decodeErr != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(decodeErr.Error()))
	}
	if err := c.validateWithDetails(dto); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}
	if err := c.accounts.VerifyEmail(ctx, dto.Token); err != nil {
		if errors.Is(err, ErrInvalidLink) {
			return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("This link is invalid or has expired. Ask for a new one."))
		}
		c.logger.Errorw("Failed to verify an email address", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Email address verified", nil))
}

// ResendVerification sends the signed-in user a new verification link, at most five times an hour
// per address and twenty per client
func (c *Controller) ResendVerification(ctx context.Context, input *ResendVerificationInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	userID, _ := httpx.Lookup(ctx, "userId")
	id, _ := userID.(string)
	if !c.accounts.Enabled() {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse(ErrAccountEmailsOff.Error()))
	}
	email, err := c.accounts.EmailOf(ctx, id)
	if err != nil {
		c.logger.Errorw("Failed to read the signed-in user", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	if email == "" {
		return nil, httpx.Fail(http.StatusUnauthorized, utils.NewFailResponse("Unauthorized"))
	}
	tooMany := tooManyRequests()
	if allowed, _ := c.resendByIP.Allow(c.clientKey(input.Request)); !allowed {
		return nil, tooMany
	}
	if allowed, _ := c.resendByEmail.Allow(strings.ToLower(email)); !allowed {
		return nil, tooMany
	}
	if err := c.accounts.ResendVerification(ctx, id); err != nil {
		switch {
		case errors.Is(err, ErrAlreadyVerified):
			return nil, httpx.Fail(http.StatusConflict, utils.NewFailResponse("This email address is verified already"))
		case errors.Is(err, ErrUserNotFound):
			return nil, httpx.Fail(http.StatusUnauthorized, utils.NewFailResponse("Unauthorized"))
		}
		c.logger.Errorw("Failed to send a verification email", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	return httpx.Reply(http.StatusAccepted, utils.NewSuccessResponse[any]("A new verification link is on its way", nil))
}
