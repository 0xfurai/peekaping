package api_key

import (
	"context"
	"errors"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/workspace/workspacehttp"
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

// fail turns the error of the service into its answer: 403 for a member, 404 for a workspace the
// request isn't in or a key it doesn't have, else 500
func fail(err error) error {
	if answer := workspacehttp.Fail(err); answer != nil {
		return answer
	}
	if errors.Is(err, ErrNotFound) {
		return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("API key not found"))
	}
	return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse(err.Error()))
}

// CreateAPIKey creates a new API key
func (c *Controller) CreateAPIKey(ctx context.Context, input *CreateAPIKeyInput) (*httpx.Output[utils.ApiResponse[*APIKeyWithTokenResponse]], error) {
	// MARK: CreateAPIKey

	req, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	// Validate the request
	validate := validator.New()
	if err := validate.Struct(req); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	// Validate expiration date
	if req.ExpiresAt != nil && req.ExpiresAt.Before(time.Now()) {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("expiration date cannot be in the past"))
	}

	// Validate max usage count
	if req.MaxUsageCount != nil && *req.MaxUsageCount <= 0 {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("max usage count must be greater than 0"))
	}

	// Convert DTO to service request
	serviceReq := &CreateRequest{
		Name:          req.Name,
		ExpiresAt:     req.ExpiresAt,
		MaxUsageCount: req.MaxUsageCount,
	}

	apiKey, err := c.service.Create(ctx, serviceReq)
	if err != nil {
		return nil, fail(err)
	}

	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("API key created successfully", apiKey.ToAPIKeyWithTokenResponse()))
}

// GetAPIKeys gets all API keys
func (c *Controller) GetAPIKeys(ctx context.Context, input *GetAPIKeysInput) (*httpx.Output[utils.ApiResponse[[]*APIKeyResponse]], error) {
	// MARK: GetAPIKeys

	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	apiKeys, err := c.service.FindAll(ctx, workspaceID)
	if err != nil {
		return nil, fail(err)
	}

	responses := make([]*APIKeyResponse, len(apiKeys))
	for i, apiKey := range apiKeys {
		responses[i] = apiKey.ToAPIKeyResponse()
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("API keys retrieved successfully", responses))
}

// GetAPIKey gets a specific API key by ID
func (c *Controller) GetAPIKey(ctx context.Context, input *GetAPIKeyInput) (*httpx.Output[utils.ApiResponse[*APIKeyResponse]], error) {
	r := input.Request
	// MARK: GetAPIKey

	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	id := r.PathValue("id")
	apiKey, err := c.service.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, fail(err)
	}
	if apiKey == nil {
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("API key not found"))
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("API key retrieved successfully", apiKey.ToAPIKeyResponse()))
}

// UpdateAPIKey updates an API key
func (c *Controller) UpdateAPIKey(ctx context.Context, input *UpdateAPIKeyInput) (*httpx.Output[utils.ApiResponse[*APIKeyResponse]], error) {
	r := input.Request
	// MARK: UpdateAPIKey

	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	id := r.PathValue("id")
	req, decodeErr := input.Decode()
	if err := decodeErr; err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	// Validate the request
	validate := validator.New()
	if err := validate.Struct(req); err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
	}

	// Validate expiration date if provided
	if req.ExpiresAt != nil && req.ExpiresAt.Before(time.Now()) {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("expiration date cannot be in the past"))
	}

	// Validate max usage count if provided
	if req.MaxUsageCount != nil && *req.MaxUsageCount <= 0 {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("max usage count must be greater than 0"))
	}

	// Convert DTO to service request
	serviceReq := &UpdateRequest{
		Name:          req.Name,
		ExpiresAt:     req.ExpiresAt,
		MaxUsageCount: req.MaxUsageCount,
	}

	apiKey, err := c.service.Update(ctx, workspaceID, id, serviceReq)
	if err != nil {
		return nil, fail(err)
	}

	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("API key updated successfully", apiKey.ToAPIKeyResponse()))
}

// DeleteAPIKey deletes an API key
func (c *Controller) DeleteAPIKey(ctx context.Context, input *DeleteAPIKeyInput) (*httpx.Output[httpx.NoBody], error) {
	r := input.Request
	// MARK: DeleteAPIKey

	workspaceID, err := workspacehttp.Scope(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.service.Delete(ctx, workspaceID, r.PathValue("id")); err != nil {
		return nil, fail(err)
	}

	return httpx.Reply(http.StatusNoContent, httpx.NoBody{})
}

// GetAPIKeyConfig gets API key configuration
func (c *Controller) GetAPIKeyConfig(ctx context.Context, input *GetAPIKeyConfigInput) (*httpx.Output[utils.ApiResponse[*APIKeyConfigResponse]], error) {

	// MARK: GetAPIKeyConfig

	config := &APIKeyConfigResponse{
		Prefix: ApiKeyPrefix,
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("API key configuration retrieved successfully", config))
}
