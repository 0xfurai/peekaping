package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacehttp"
	"peekaping/internal/ratelimit"
	"peekaping/internal/utils"
	"peekaping/internal/validation"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
)

type Controller struct {
	service  Service
	accounts *AccountService
	signup   *SignupGate
	proxies  *httpx.TrustedProxies
	logger   *zap.SugaredLogger

	// The limits on the routes that send an email, per email address and per client address. They
	// count in this process's memory (see package ratelimit).
	forgotByEmail, forgotByIP, resendByEmail, resendByIP *ratelimit.Limiter

	// The limits on the routes that spend a link, per client address. A request counts whether
	// its token is good or not: the limit is on guessing.
	resetByIP, verifyByIP *ratelimit.Limiter
}

// The most emails the account routes send per hour to one address, and per hour from one client
const (
	maxAccountEmailsPerAddress = 5
	maxAccountEmailsPerClient  = 20
	// maxLinkAttemptsPerClient is the most requests per hour from one client to spend a reset or verification link
	maxLinkAttemptsPerClient = 20
)

func NewController(
	service Service,
	accounts *AccountService,
	signup *SignupGate,
	proxies *httpx.TrustedProxies,
	logger *zap.SugaredLogger,
) *Controller {
	return &Controller{
		service:  service,
		accounts: accounts,
		signup:   signup,
		proxies:  proxies,
		logger:   logger,

		forgotByEmail: ratelimit.New(maxAccountEmailsPerAddress, time.Hour),
		forgotByIP:    ratelimit.New(maxAccountEmailsPerClient, time.Hour),
		resendByEmail: ratelimit.New(maxAccountEmailsPerAddress, time.Hour),
		resendByIP:    ratelimit.New(maxAccountEmailsPerClient, time.Hour),
		resetByIP:     ratelimit.New(maxLinkAttemptsPerClient, time.Hour),
		verifyByIP:    ratelimit.New(maxLinkAttemptsPerClient, time.Hour),
	}
}

// validateWithDetails provides detailed error messages for validation failures
func (c *Controller) validateWithDetails(dto interface{}) error {
	if err := validation.Validate.Struct(dto); err != nil {
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			var errorMessages []string

			for _, fieldError := range validationErrors {
				field := fieldError.Field()
				tag := fieldError.Tag()

				switch tag {
				case "required":
					errorMessages = append(errorMessages, fmt.Sprintf("%s is required", field))
				case "email":
					errorMessages = append(errorMessages, "Please provide a valid email address")
				case "password":
					errorMessages = append(errorMessages, "Password must be at least 8 characters long and contain uppercase, lowercase, number, and special character")
				default:
					errorMessages = append(errorMessages, fmt.Sprintf("%s validation failed", field))
				}
			}

			return errors.New(strings.Join(errorMessages, "; "))
		}
		return err
	}
	return nil
}
func (c *Controller) Register(ctx context.Context, input *RegisterInput) (*httpx.Output[utils.ApiResponse[*LoginResponse]], error) {
	dto, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	// validate with detailed error messages
	if err := c.validateWithDetails(dto); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	// An open signup passes the gate: a limit per client and, when set up, Turnstile. An invite signup
	// does not: the invite is the proof that the visitor is wanted.
	if dto.InviteToken == "" && c.signup.Open() {
		if err := c.signup.Admit(ctx, c.clientKey(input.Request), c.proxies.ClientIP(input.Request), dto.TurnstileToken); err != nil {
			if errors.Is(err, ErrSignupRateLimited) {
				return nil, tooManyRequests()
			}
			return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("We could not verify that you are not a robot. Reload the page and try again."))
		}
	}

	response, err := c.service.Register(ctx, dto)
	if err != nil {
		// Signing up with an address that has an account is for the visitor to fix, with a way out
		if errors.Is(err, ErrEmailTaken) && dto.InviteToken == "" {
			c.logger.Infow("Registration refused: an account with this email exists")
			return nil, httpx.Fail(http.StatusConflict, utils.NewFailResponse("An account with this email address exists already: sign in instead"))
		}
		if errors.Is(err, ErrAdminExists) {
			c.logger.Infow("Registration refused: the install has its admin already")
			return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		}
		// An invite that doesn't exist, can't be used or names another email is the visitor's
		// problem, not the server's: it is logged as the refusal it is, without the token
		if reason := inviteRefusal(err); reason != "" {
			c.logger.Infow("Invite refused: " + reason)
			if errors.Is(err, ErrEmailTaken) {
				return nil, httpx.Fail(http.StatusConflict, utils.NewFailResponse("An account with this email exists: sign in and accept the invite"))
			}
			if answer := workspacehttp.Fail(err); answer != nil {
				return nil, answer
			}
		}
		c.logger.Errorw("Failed to register", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
	}

	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("User registered successfully", response))
}

// inviteRefusal says why a signup through an invite was refused, or returns "" for a failure
// that is not a refusal
func inviteRefusal(err error) string {
	switch {
	case errors.Is(err, workspace.ErrInviteUsed):
		return "already used"
	case errors.Is(err, workspace.ErrInviteExpired):
		return "expired"
	case errors.Is(err, workspace.ErrInviteRevoked):
		return "revoked"
	case errors.Is(err, workspace.ErrInviteUnavailable):
		return "can't be used"
	case errors.Is(err, workspace.ErrInviteNotFound):
		return "not found"
	case errors.Is(err, workspace.ErrInviteEmailMismatch):
		return "for another email address"
	case errors.Is(err, workspace.ErrAlreadyMember):
		return "already a member of the workspace"
	case errors.Is(err, ErrEmailTaken):
		return "an account with this email exists"
	}
	return ""
}

func (c *Controller) Login(ctx context.Context, input *LoginInput) (*httpx.Output[utils.ApiResponse[*LoginResponse]], error) {
	dto, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := c.validateWithDetails(dto); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	response, err := c.service.Login(ctx, dto)
	if err != nil {
		c.logger.Errorw("Failed to login admin", "error", err)
		return nil, httpx.Fail(http.StatusUnauthorized, utils.NewFailResponse(err.Error()))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Login successful", response))
}
func (c *Controller) RefreshToken(ctx context.Context, input *RefreshTokenInput) (*httpx.Output[utils.ApiResponse[*LoginResponse]], error) {
	dto, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := c.validateWithDetails(dto); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	response, err := c.service.RefreshToken(ctx, dto.RefreshToken)
	if err != nil {
		c.logger.Errorw("Failed to refresh token", "error", err)
		return nil, httpx.Fail(http.StatusUnauthorized, utils.NewFailResponse(err.Error()))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Token refreshed successfully", response))
}
func (c *Controller) UpdatePassword(ctx context.Context, input *UpdatePasswordInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	userId, exists := httpx.Lookup(ctx, "userId")
	if !exists {
		return nil, httpx.Fail(http.StatusUnauthorized, utils.NewFailResponse("Unauthorized"))
	}

	dto, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := c.validateWithDetails(dto); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	err := c.service.UpdatePassword(ctx, userId.(string), dto)
	if err != nil {
		if err.Error() == "current password is incorrect" {
			return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		}
		c.logger.Errorw("Failed to update password", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("Password updated successfully", nil))
}

// Me answers with the signed-in user and their workspaces. It needs no workspace: a user in none
// gets an empty list.
func (c *Controller) Me(ctx context.Context, _ *MeInput) (*httpx.Output[utils.ApiResponse[*MeResponse]], error) {
	userId, _ := httpx.Lookup(ctx, "userId")
	id, _ := userId.(string)
	me, err := c.service.Me(ctx, id)
	if err != nil {
		c.logger.Errorw("Failed to read the signed-in user", "error", err)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
	}
	if me == nil {
		return nil, httpx.Fail(http.StatusUnauthorized, utils.NewFailResponse("Unauthorized"))
	}
	out, err := httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Signed-in user retrieved successfully", me))
	if err != nil {
		return nil, err
	}
	// The answer names the user and their workspaces, so a browser must not keep it on disk
	noStore := "no-store"
	out.CacheControl = &noStore
	return out, nil
}
func (c *Controller) SetupTwoFA(ctx context.Context, input *SetupTwoFAInput) (*httpx.Output[TwoFASetupResponseDto], error) {
	userId, _ := httpx.Lookup(ctx, "userId")

	dto, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := c.validateWithDetails(dto); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	secret, provisioningURI, err := c.service.SetupTwoFA(ctx, userId.(string), dto.Password)
	if err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}
	return httpx.Reply(http.StatusOK, TwoFASetupResponseDto{
		Secret:          secret,
		ProvisioningURI: provisioningURI,
	})
}
func (c *Controller) VerifyTwoFA(ctx context.Context, input *VerifyTwoFAInput) (*httpx.Output[TwoFAVerifyResponseDto], error) {
	userId, _ := httpx.Lookup(ctx, "userId")

	dto, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	if err := c.validateWithDetails(dto); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	success, err := c.service.VerifyTwoFA(ctx, userId.(string), dto.Code)
	if err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, TwoFAVerifyResponseDto{Success: false, Message: err.Error()})
	}

	if !success {
		return nil, httpx.Fail(http.StatusBadRequest, TwoFAVerifyResponseDto{Success: false, Message: "invalid 2FA code"})
	}

	return httpx.Reply(http.StatusOK, TwoFAVerifyResponseDto{Success: true, Message: "2FA verification successful"})
}
func (c *Controller) DisableTwoFA(ctx context.Context, input *DisableTwoFAInput) (*httpx.Output[utils.ApiResponse[any]], error) {
	userId, _ := httpx.Lookup(ctx, "userId")

	dto, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}

	if err := c.validateWithDetails(dto); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	err := c.service.DisableTwoFA(ctx, userId.(string), dto.Password)
	if err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse[any]("2FA disabled successfully", nil))
}
