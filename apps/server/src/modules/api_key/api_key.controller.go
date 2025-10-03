package api_key

import (
	"net/http"
	"peekaping/src/utils"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

type Controller struct {
	service Service
}

func NewController(service Service) *Controller {
	return &Controller{
		service: service,
	}
}

// CreateAPIKey creates a new API key
// @Summary Create API key
// @Description Create a new API key for the authenticated user
// @Tags api-keys
// @Accept json
// @Produce json
// @Param request body CreateAPIKeyDto true "API key creation data"
// @Success 201 {object} utils.ApiResponse[APIKeyWithTokenResponse]
// @Failure 400 {object} utils.APIError
// @Failure 401 {object} utils.APIError
// @Failure 500 {object} utils.APIError
// @Router /api-keys [post]
// @Security BearerAuth
func (c *Controller) CreateAPIKey(ctx *gin.Context) {
	userID, exists := ctx.Get("userId")
	if !exists {
		ctx.JSON(http.StatusUnauthorized, utils.NewFailResponse("User not authenticated"))
		return
	}

	var req CreateAPIKeyDto
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid request data"))
		return
	}

	// Validate the request
	validate := validator.New()
	if err := validate.Struct(req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Validation failed: " + err.Error()))
		return
	}

	// Convert DTO to service request
	serviceReq := &CreateRequest{
		Name:          req.Name,
		ExpiresAt:     req.ExpiresAt,
		MaxUsageCount: req.MaxUsageCount,
	}

	apiKey, err := c.service.Create(ctx, userID.(string), serviceReq)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	ctx.JSON(http.StatusCreated, utils.NewSuccessResponse("API key created successfully", apiKey.ToAPIKeyWithTokenResponse()))
}

// GetAPIKeys gets all API keys for the authenticated user
// @Summary Get API keys
// @Description Get all API keys for the authenticated user
// @Tags api-keys
// @Produce json
// @Success 200 {object} utils.ApiResponse[[]APIKeyResponse]
// @Failure 401 {object} utils.APIError
// @Failure 500 {object} utils.APIError
// @Router /api-keys [get]
// @Security BearerAuth
func (c *Controller) GetAPIKeys(ctx *gin.Context) {
	userID, exists := ctx.Get("userId")
	if !exists {
		ctx.JSON(http.StatusUnauthorized, utils.NewFailResponse("User not authenticated"))
		return
	}

	apiKeys, err := c.service.FindByUserID(ctx, userID.(string))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Failed to fetch API keys"))
		return
	}

	responses := make([]*APIKeyResponse, len(apiKeys))
	for i, apiKey := range apiKeys {
		responses[i] = apiKey.ToAPIKeyResponse()
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("API keys retrieved successfully", responses))
}

// GetAPIKey gets a specific API key by ID
// @Summary Get API key
// @Description Get a specific API key by ID
// @Tags api-keys
// @Produce json
// @Param id path string true "API key ID"
// @Success 200 {object} utils.ApiResponse[APIKeyResponse]
// @Failure 401 {object} utils.APIError
// @Failure 404 {object} utils.APIError
// @Failure 500 {object} utils.APIError
// @Router /api-keys/{id} [get]
// @Security BearerAuth
func (c *Controller) GetAPIKey(ctx *gin.Context) {
	userID, exists := ctx.Get("userId")
	if !exists {
		ctx.JSON(http.StatusUnauthorized, utils.NewFailResponse("User not authenticated"))
		return
	}

	id := ctx.Param("id")
	apiKey, err := c.service.FindByID(ctx, id)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Failed to fetch API key"))
		return
	}
	if apiKey == nil {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("API key not found"))
		return
	}

	// Verify the API key belongs to the user
	if apiKey.UserID != userID.(string) {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("API key not found"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("API key retrieved successfully", apiKey.ToAPIKeyResponse()))
}

// UpdateAPIKey updates an API key
// @Summary Update API key
// @Description Update an API key
// @Tags api-keys
// @Accept json
// @Produce json
// @Param id path string true "API key ID"
// @Param request body UpdateAPIKeyDto true "API key update data"
// @Success 200 {object} utils.ApiResponse[APIKeyResponse]
// @Failure 400 {object} utils.APIError
// @Failure 401 {object} utils.APIError
// @Failure 404 {object} utils.APIError
// @Failure 500 {object} utils.APIError
// @Router /api-keys/{id} [put]
// @Security BearerAuth
func (c *Controller) UpdateAPIKey(ctx *gin.Context) {
	userID, exists := ctx.Get("userId")
	if !exists {
		ctx.JSON(http.StatusUnauthorized, utils.NewFailResponse("User not authenticated"))
		return
	}

	id := ctx.Param("id")
	var req UpdateAPIKeyDto
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid request data"))
		return
	}

	// Validate the request
	validate := validator.New()
	if err := validate.Struct(req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Validation failed: " + err.Error()))
		return
	}

	// Convert DTO to service request
	serviceReq := &UpdateRequest{
		Name:          req.Name,
		ExpiresAt:     req.ExpiresAt,
		MaxUsageCount: req.MaxUsageCount,
	}

	apiKey, err := c.service.Update(ctx, id, userID.(string), serviceReq)
	if err != nil {
		if err.Error() == "API key not found" || err.Error() == "unauthorized" {
			ctx.JSON(http.StatusNotFound, utils.NewFailResponse("API key not found"))
			return
		}
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("API key updated successfully", apiKey.ToAPIKeyResponse()))
}

// DeleteAPIKey deletes an API key
// @Summary Delete API key
// @Description Delete an API key
// @Tags api-keys
// @Param id path string true "API key ID"
// @Success 204 "No Content"
// @Failure 401 {object} utils.APIError
// @Failure 404 {object} utils.APIError
// @Failure 500 {object} utils.APIError
// @Router /api-keys/{id} [delete]
// @Security BearerAuth
func (c *Controller) DeleteAPIKey(ctx *gin.Context) {
	userID, exists := ctx.Get("userId")
	if !exists {
		ctx.JSON(http.StatusUnauthorized, utils.NewFailResponse("User not authenticated"))
		return
	}

	id := ctx.Param("id")
	err := c.service.Delete(ctx, id, userID.(string))
	if err != nil {
		if err.Error() == "API key not found" || err.Error() == "unauthorized" {
			ctx.JSON(http.StatusNotFound, utils.NewFailResponse("API key not found"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Failed to delete API key"))
		return
	}

	ctx.Status(http.StatusNoContent)
}

// GetAPIKeyConfig gets API key configuration
// @Summary Get API key configuration
// @Description Get API key configuration including prefix
// @Tags api-keys
// @Produce json
// @Success 200 {object} utils.ApiResponse[APIKeyConfigResponse]
// @Router /api-keys/config [get]
func (c *Controller) GetAPIKeyConfig(ctx *gin.Context) {
	config := &APIKeyConfigResponse{
		Prefix: ApiKeyPrefix,
	}
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("API key configuration retrieved successfully", config))
}
