package api_key

import (
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/utils"
	"time"

	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
)

type Controller struct {
	service Service
	logger  *zap.SugaredLogger
}

func NewController(service Service, logger *zap.SugaredLogger) *Controller {
	return &Controller{
		service: service,
		logger:  logger.Named("[api-key-controller]"),
	}
}

// CreateAPIKey creates a new API key
// @Summary Create API key
// @Description Create a new API key
// @Tags api-keys
// @Accept json
// @Produce json
// @Security JwtAuth
// @Param request body CreateAPIKeyDto true "API key creation data"
// @Success 201 {object} utils.ApiResponse[APIKeyWithTokenResponse]
// @Failure 400 {object} utils.APIError
// @Failure 500 {object} utils.APIError
// @Router /api-keys [post]
func (c *Controller) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// MARK: CreateAPIKey

	var req CreateAPIKeyDto
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Validate the request
	validate := validator.New()
	if err := validate.Struct(req); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Validate expiration date
	if req.ExpiresAt != nil && req.ExpiresAt.Before(time.Now()) {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse("expiration date cannot be in the past"))
		return
	}

	// Validate max usage count
	if req.MaxUsageCount != nil && *req.MaxUsageCount <= 0 {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse("max usage count must be greater than 0"))
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
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
		return
	}

	httpx.JSON(w, http.StatusCreated, utils.NewSuccessResponse("API key created successfully", apiKey.ToAPIKeyWithTokenResponse()))
}

// GetAPIKeys gets all API keys
// @Summary Get API keys
// @Description Get all API keys
// @Tags api-keys
// @Produce json
// @Security JwtAuth
// @Success 200 {object} utils.ApiResponse[[]APIKeyResponse]
// @Failure 500 {object} utils.APIError
// @Router /api-keys [get]
func (c *Controller) GetAPIKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// MARK: GetAPIKeys

	apiKeys, err := c.service.FindAll(ctx)
	if err != nil {
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
		return
	}

	responses := make([]*APIKeyResponse, len(apiKeys))
	for i, apiKey := range apiKeys {
		responses[i] = apiKey.ToAPIKeyResponse()
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("API keys retrieved successfully", responses))
}

// GetAPIKey gets a specific API key by ID
// @Summary Get API key
// @Description Get a specific API key by ID
// @Tags api-keys
// @Produce json
// @Security JwtAuth
// @Param id path string true "API key ID"
// @Success 200 {object} utils.ApiResponse[APIKeyResponse]
// @Failure 404 {object} utils.APIError
// @Failure 500 {object} utils.APIError
// @Router /api-keys/{id} [get]
func (c *Controller) GetAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// MARK: GetAPIKey

	id := r.PathValue("id")
	apiKey, err := c.service.FindByID(ctx, id)
	if err != nil {
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
		return
	}
	if apiKey == nil {
		httpx.JSON(w, http.StatusNotFound, utils.NewFailResponse("API key not found"))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("API key retrieved successfully", apiKey.ToAPIKeyResponse()))
}

// UpdateAPIKey updates an API key
// @Summary Update API key
// @Description Update an API key
// @Tags api-keys
// @Accept json
// @Produce json
// @Security JwtAuth
// @Param id path string true "API key ID"
// @Param request body UpdateAPIKeyDto true "API key update data"
// @Success 200 {object} utils.ApiResponse[APIKeyResponse]
// @Failure 400 {object} utils.APIError
// @Failure 404 {object} utils.APIError
// @Failure 500 {object} utils.APIError
// @Router /api-keys/{id} [put]
func (c *Controller) UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// MARK: UpdateAPIKey

	id := r.PathValue("id")
	var req UpdateAPIKeyDto
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Validate the request
	validate := validator.New()
	if err := validate.Struct(req); err != nil {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// Validate expiration date if provided
	if req.ExpiresAt != nil && req.ExpiresAt.Before(time.Now()) {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse("expiration date cannot be in the past"))
		return
	}

	// Validate max usage count if provided
	if req.MaxUsageCount != nil && *req.MaxUsageCount <= 0 {
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse("max usage count must be greater than 0"))
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
			httpx.JSON(w, http.StatusNotFound, utils.NewFailResponse("API key not found"))
			return
		}
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
		return
	}

	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("API key updated successfully", apiKey.ToAPIKeyResponse()))
}

// DeleteAPIKey deletes an API key
// @Summary Delete API key
// @Description Delete an API key
// @Tags api-keys
// @Security JwtAuth
// @Param id path string true "API key ID"
// @Success 204 "No Content"
// @Failure 404 {object} utils.APIError
// @Failure 500 {object} utils.APIError
// @Router /api-keys/{id} [delete]
func (c *Controller) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// MARK: DeleteAPIKey

	id := r.PathValue("id")
	if _, ok := utils.FindOr404(ctx, w, c.logger, "API key", c.service.FindByID, id); !ok {
		return
	}

	err := c.service.Delete(ctx, id)
	if err != nil {
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetAPIKeyConfig gets API key configuration
// @Summary Get API key configuration
// @Description Get API key configuration including prefix
// @Tags api-keys
// @Produce json
// @Success 200 {object} utils.ApiResponse[APIKeyConfigResponse]
// @Router /api-keys/config [get]
func (c *Controller) GetAPIKeyConfig(w http.ResponseWriter, r *http.Request) {

	// MARK: GetAPIKeyConfig

	config := &APIKeyConfigResponse{
		Prefix: ApiKeyPrefix,
	}
	httpx.JSON(w, http.StatusOK, utils.NewSuccessResponse("API key configuration retrieved successfully", config))
}
