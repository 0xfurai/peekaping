package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/utils"
	"peekaping/internal/validation"
	"strings"

	"github.com/go-playground/validator/v10"
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
		service: service,
		logger:  logger,
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

	response, err := c.service.Register(ctx, dto)
	if err != nil {
		c.logger.Errorw("Failed to register admin", "error", err)
		if errors.Is(err, ErrAdminExists) {
			return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		}
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
	}

	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("User registered successfully", response))
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

	return httpx.Reply(http.StatusOK, TwoFAVerifyResponseDto{Success: success, Message: "2FA verification successful"})
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
