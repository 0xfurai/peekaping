package auth

import (
	"errors"
	"fmt"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/utils"
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
	if err := utils.Validate.Struct(dto); err != nil {
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

// @Router		/auth/register [post]
// @Summary		Register new admin
// @Tags			Auth
// @Produce		json
// @Accept		json
// @Param       body body     RegisterDto  true  "Registration data"
// @Success		201	{object}	utils.ApiResponse[LoginResponse]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) Register(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var dto RegisterDto
	if err := httpx.DecodeJSON(r, &dto); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// validate with detailed error messages
	if err := c.validateWithDetails(dto); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	response, err := c.service.Register(ctx, dto)
	if err != nil {
		c.logger.Errorw("Failed to register admin", "error", err)
		if err.Error() == "admin already exists" {
			httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
			return
		}
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
		return
	}

	httpx.JSON(w, http.StatusCreated, utils.NewSuccessResponse("User registered successfully", response))
}

// @Router		/auth/login [post]
// @Summary		Login admin
// @Tags			Auth
// @Produce		json
// @Accept		json
// @Param       body body     LoginDto  true  "Login data"
// @Success		200	{object}	utils.ApiResponse[LoginResponse]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		401	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var dto LoginDto
	if err := httpx.DecodeJSON(r, &dto); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := c.validateWithDetails(dto); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	response, err := c.service.Login(ctx, dto)
	if err != nil {
		c.logger.Errorw("Failed to login admin", "error", err)
		httpx.JSON(w, http.StatusUnauthorized, utils.NewFailResponse(err.Error()))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("Login successful", response))
}

// @Router		/auth/refresh [post]
// @Summary		Refresh access token
// @Tags			Auth
// @Produce		json
// @Accept		json
// @Param       body body     RefreshTokenDto  true  "Refresh token data"
// @Success		200	{object}	utils.ApiResponse[LoginResponse]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		401	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) RefreshToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var dto RefreshTokenDto
	if err := httpx.DecodeJSON(r, &dto); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := c.validateWithDetails(dto); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	response, err := c.service.RefreshToken(ctx, dto.RefreshToken)
	if err != nil {
		c.logger.Errorw("Failed to refresh token", "error", err)
		httpx.JSON(w, http.StatusUnauthorized, utils.NewFailResponse(err.Error()))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("Token refreshed successfully", response))
}

// @Router	/auth/password [put]
// @Summary	Update user password
// @Tags		Auth
// @Produce	json
// @Accept	json
// @Security JwtAuth
// @Param	body body     UpdatePasswordDto  true  "Password update data"
// @Success	200	{object}	utils.ApiResponse[any]
// @Failure	400	{object}	utils.APIError[any]
// @Failure	401	{object}	utils.APIError[any]
// @Failure	500	{object}	utils.APIError[any]
func (c *Controller) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId, exists := httpx.Lookup(ctx, "userId")
	if !exists {
		httpx.JSON(w, http.StatusUnauthorized, utils.NewFailResponse("Unauthorized"))
		return
	}

	var dto UpdatePasswordDto
	if err := httpx.DecodeJSON(r, &dto); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := c.validateWithDetails(dto); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	err := c.service.UpdatePassword(ctx, userId.(string), dto)
	if err != nil {
		if err.Error() == "current password is incorrect" {
			httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
			return
		}
		c.logger.Errorw("Failed to update password", "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse[any]("Password updated successfully", nil))
}

// @Router	/auth/2fa/setup [post]
// @Summary	Enable 2FA (TOTP) for user
// @Tags		Auth
// @Produce	json
// @Accept	json
// @Security JwtAuth
// @Param	body body     TwoFASetupRequestDto  true  "2FA setup request"
// @Success	200 {object} TwoFASetupResponseDto
// @Failure	400 {object} utils.APIError[any]
// @Failure	500 {object} utils.APIError[any]
func (c *Controller) SetupTwoFA(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId, _ := httpx.Lookup(ctx, "userId")

	var dto TwoFASetupRequestDto
	if err := httpx.DecodeJSON(r, &dto); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := c.validateWithDetails(dto); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	secret, provisioningURI, err := c.service.SetupTwoFA(ctx, userId.(string), dto.Password)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}
	httpx.JSON(w, http.StatusOK, TwoFASetupResponseDto{
		Secret:          secret,
		ProvisioningURI: provisioningURI,
	})
}

// @Router	/auth/2fa/verify [post]
// @Summary	Verify 2FA (TOTP) code for user
// @Tags		Auth
// @Produce	json
// @Accept	json
// @Security JwtAuth
// @Param	body body     TwoFAVerifyRequestDto  true  "2FA verify request"
// @Success	200 {object} TwoFAVerifyResponseDto
// @Failure	400 {object} TwoFAVerifyResponseDto
// @Failure	500 {object} utils.APIError[any]
func (c *Controller) VerifyTwoFA(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId, _ := httpx.Lookup(ctx, "userId")

	var dto TwoFAVerifyRequestDto
	if err := httpx.DecodeJSON(r, &dto); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := c.validateWithDetails(dto); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	success, err := c.service.VerifyTwoFA(ctx, userId.(string), dto.Code)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, TwoFAVerifyResponseDto{Success: false, Message: err.Error()})
		return
	}

	httpx.JSON(w, http.StatusOK, TwoFAVerifyResponseDto{Success: success, Message: "2FA verification successful"})
}

// @Router	/auth/2fa/disable [post]
// @Summary	Disable 2FA (TOTP) for user
// @Tags		Auth
// @Produce	json
// @Accept	json
// @Security JwtAuth
// @Param	body body     TwoFADisableRequestDto  true  "2FA disable request"
// @Success	200 {object} utils.ApiResponse[any]
// @Failure	400 {object} utils.APIError[any]
// @Failure	500 {object} utils.APIError[any]
func (c *Controller) DisableTwoFA(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId, _ := httpx.Lookup(ctx, "userId")

	var dto TwoFADisableRequestDto
	if err := httpx.DecodeJSON(r, &dto); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
		return
	}

	if err := c.validateWithDetails(dto); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	err := c.service.DisableTwoFA(ctx, userId.(string), dto.Password)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}
	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse[any]("2FA disabled successfully", nil))
}
