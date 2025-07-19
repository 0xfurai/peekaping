package auth

import (
	"errors"
	"fmt"
	"net/http"
	"peekaping/src/utils"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
	"peekaping/src/modules/auth/login_attempt"
)

type Controller struct {
	service    Service
	middleware *MiddlewareProvider
	logger     *zap.SugaredLogger
}

func NewController(
	service Service,
	middleware *MiddlewareProvider,
	logger *zap.SugaredLogger,
) *Controller {
	return &Controller{
		service:    service,
		middleware: middleware,
		logger:     logger,
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
func (c *Controller) Register(ctx *gin.Context) {
	var dto RegisterDto
	if err := ctx.ShouldBindJSON(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// validate with detailed error messages
	if err := c.validateWithDetails(dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	response, err := c.service.Register(ctx, dto)
	if err != nil {
		c.logger.Errorw("Failed to register admin", "error", err)
		if err.Error() == "admin already exists" {
			ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
			return
		}
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
		return
	}

	ctx.JSON(http.StatusCreated, utils.NewSuccessResponse("User registered successfully", response))
}

// @Router		/auth/login [post]
// @Summary		Login admin
// @Tags			Auth
// @Produce		json
// @Accept		json
// @Param       body body     LoginDto  true  "Login data"
// @Success		200	{object}	utils.ApiResponse[LoginResponse]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *Controller) Login(ctx *gin.Context) {
	var dto LoginDto

	// Check if the DTO was already bound by the middleware
	if cachedDto, exists := ctx.Get("login_dto"); exists {
		middlewareDto := cachedDto.(login_attempt.LoginDto)
		// Convert from middleware DTO to auth DTO
		dto = LoginDto{
			Email:    middlewareDto.Email,
			Password: middlewareDto.Password,
			Token:    middlewareDto.Token,
		}
	} else {
		if err := ctx.ShouldBindJSON(&dto); err != nil {
			ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
			return
		}
	}

	if err := c.validateWithDetails(dto); err != nil {
		c.middleware.RecordLoginAttempt(ctx, dto.Email, false)
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	response, err := c.service.Login(ctx, dto)
	if err != nil {
		c.logger.Errorw("Failed to login admin", "error", err)
		// Record failed login attempt
		c.middleware.RecordLoginAttempt(ctx, dto.Email, false)
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Record successful login attempt
	c.middleware.RecordLoginAttempt(ctx, dto.Email, true)
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("Login successful", response))
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
func (c *Controller) RefreshToken(ctx *gin.Context) {
	var dto RefreshTokenDto
	if err := ctx.ShouldBindJSON(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := c.validateWithDetails(dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	response, err := c.service.RefreshToken(ctx, dto.RefreshToken)
	if err != nil {
		c.logger.Errorw("Failed to refresh token", "error", err)
		ctx.JSON(http.StatusUnauthorized, utils.NewFailResponse(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("Token refreshed successfully", response))
}

// @Router	/auth/password [put]
// @Summary	Update user password
// @Tags		Auth
// @Produce	json
// @Accept	json
// @Param	body body     UpdatePasswordDto  true  "Password update data"
// @Success	200	{object}	utils.ApiResponse[any]
// @Failure	400	{object}	utils.APIError[any]
// @Failure	401	{object}	utils.APIError[any]
// @Failure	500	{object}	utils.APIError[any]
func (c *Controller) UpdatePassword(ctx *gin.Context) {
	userId, exists := ctx.Get("userId")
	if !exists {
		ctx.JSON(http.StatusUnauthorized, utils.NewFailResponse("Unauthorized"))
		return
	}

	var dto UpdatePasswordDto
	if err := ctx.ShouldBindJSON(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := c.validateWithDetails(dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	err := c.service.UpdatePassword(ctx, userId.(string), dto)
	if err != nil {
		if err.Error() == "current password is incorrect" {
			ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
			return
		}
		c.logger.Errorw("Failed to update password", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse[any]("Password updated successfully", nil))
}

// @Router	/auth/2fa/setup [post]
// @Summary	Enable 2FA (TOTP) for user
// @Tags		Auth
// @Produce	json
// @Accept	json
// @Param	body body     TwoFASetupRequestDto  true  "2FA setup request"
// @Success	200 {object} TwoFASetupResponseDto
// @Failure	400 {object} utils.APIError[any]
// @Failure	500 {object} utils.APIError[any]
func (c *Controller) SetupTwoFA(ctx *gin.Context) {
	userId, _ := ctx.Get("userId")

	var dto TwoFASetupRequestDto
	if err := ctx.ShouldBindJSON(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := c.validateWithDetails(dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	secret, provisioningURI, err := c.service.SetupTwoFA(ctx, userId.(string), dto.Password)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, TwoFASetupResponseDto{
		Secret:          secret,
		ProvisioningURI: provisioningURI,
	})
}

// @Router	/auth/2fa/verify [post]
// @Summary	Verify 2FA (TOTP) code for user
// @Tags		Auth
// @Produce	json
// @Accept	json
// @Param	body body     TwoFAVerifyRequestDto  true  "2FA verify request"
// @Success	200 {object} TwoFAVerifyResponseDto
// @Failure	400 {object} TwoFAVerifyResponseDto
// @Failure	500 {object} utils.APIError[any]
func (c *Controller) VerifyTwoFA(ctx *gin.Context) {
	userId, _ := ctx.Get("userId")

	var dto TwoFAVerifyRequestDto
	if err := ctx.ShouldBindJSON(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := c.validateWithDetails(dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	success, err := c.service.VerifyTwoFA(ctx, userId.(string), dto.Code)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, TwoFAVerifyResponseDto{Success: false, Message: err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, TwoFAVerifyResponseDto{Success: success, Message: "2FA verification successful"})
}

// @Router	/auth/2fa/disable [post]
// @Summary	Disable 2FA (TOTP) for user
// @Tags		Auth
// @Produce	json
// @Accept	json
// @Param	body body     TwoFADisableRequestDto  true  "2FA disable request"
// @Success	200 {object} utils.ApiResponse[any]
// @Failure	400 {object} utils.APIError[any]
// @Failure	500 {object} utils.APIError[any]
func (c *Controller) DisableTwoFA(ctx *gin.Context) {
	userId, _ := ctx.Get("userId")

	var dto TwoFADisableRequestDto
	if err := ctx.ShouldBindJSON(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
		return
	}

	if err := c.validateWithDetails(dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	err := c.service.DisableTwoFA(ctx, userId.(string), dto.Password)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse[any]("2FA disabled successfully", nil))
}
