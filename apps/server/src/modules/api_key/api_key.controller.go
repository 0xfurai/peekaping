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
// @Description Create a new API key
// @Tags api-keys
// @Accept json
// @Produce json
// @Param request body CreateAPIKeyDto true "API key creation data"
// @Success 201 {object} utils.ApiResponse[APIKeyWithTokenResponse]
// @Failure 400 {object} utils.APIError
// @Failure 500 {object} utils.APIError
// @Router /api-keys [post]
func (c *Controller) CreateAPIKey(ctx *gin.Context) {
	// MARK: CreateAPIKey

	var req CreateAPIKeyDto
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Validate the request
	validate := validator.New()
	if err := validate.Struct(req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Convert DTO to service request
	serviceReq := &CreateRequest{
		Name:          req.Name,
		ExpiresAt:     req.ExpiresAt,
		MaxUsageCount: req.MaxUsageCount,
	}

	apiKey, err := c.service.Create(ctx, serviceReq)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
		return
	}

	ctx.JSON(http.StatusCreated, utils.NewSuccessResponse("API key created successfully", apiKey.ToAPIKeyWithTokenResponse()))
}

// GetAPIKeys gets all API keys
// @Summary Get API keys
// @Description Get all API keys
// @Tags api-keys
// @Produce json
// @Success 200 {object} utils.ApiResponse[[]APIKeyResponse]
// @Failure 500 {object} utils.APIError
// @Router /api-keys [get]
func (c *Controller) GetAPIKeys(ctx *gin.Context) {
	// MARK: GetAPIKeys

	apiKeys, err := c.service.FindAll(ctx)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
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
// @Failure 404 {object} utils.APIError
// @Failure 500 {object} utils.APIError
// @Router /api-keys/{id} [get]
func (c *Controller) GetAPIKey(ctx *gin.Context) {
	// MARK: GetAPIKey

	id := ctx.Param("id")
	apiKey, err := c.service.FindByID(ctx, id)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
		return
	}
	if apiKey == nil {
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
// @Failure 404 {object} utils.APIError
// @Failure 500 {object} utils.APIError
// @Router /api-keys/{id} [put]
func (c *Controller) UpdateAPIKey(ctx *gin.Context) {
	// MARK: UpdateAPIKey

	id := ctx.Param("id")
	var req UpdateAPIKeyDto
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Validate the request
	validate := validator.New()
	if err := validate.Struct(req); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Convert DTO to service request
	serviceReq := &UpdateRequest{
		Name:          req.Name,
		ExpiresAt:     req.ExpiresAt,
		MaxUsageCount: req.MaxUsageCount,
	}

	apiKey, err := c.service.Update(ctx, id, serviceReq)
	if err != nil {
		if err.Error() == "API key not found" {
			ctx.JSON(http.StatusNotFound, utils.NewFailResponse("API key not found"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
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
// @Failure 404 {object} utils.APIError
// @Failure 500 {object} utils.APIError
// @Router /api-keys/{id} [delete]
func (c *Controller) DeleteAPIKey(ctx *gin.Context) {
	// MARK: DeleteAPIKey

	id := ctx.Param("id")
	err := c.service.Delete(ctx, id)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
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
	// MARK: GetAPIKeyConfig

	config := &APIKeyConfigResponse{
		Prefix: ApiKeyPrefix,
	}
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("API key configuration retrieved successfully", config))
}
