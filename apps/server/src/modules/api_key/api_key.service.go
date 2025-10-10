package api_key

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// Service defines the interface for API key business logic
type Service interface {
	Create(ctx context.Context, req *CreateRequest) (*APIKeyWithToken, error)
	FindAll(ctx context.Context) ([]*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	Update(ctx context.Context, id string, req *UpdateRequest) (*Model, error)
	Delete(ctx context.Context, id string) error
	ValidateKey(ctx context.Context, key string) (*Model, error)
}

// CreateRequest represents the request to create an API key
type CreateRequest struct {
	Name          string     `json:"name" validate:"required,min=1,max=255"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	MaxUsageCount *int64     `json:"max_usage_count,omitempty"`
}

// UpdateRequest represents the request to update an API key
type UpdateRequest struct {
	Name          *string     `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	ExpiresAt     *time.Time  `json:"expires_at,omitempty"`
	MaxUsageCount *int64      `json:"max_usage_count,omitempty"`
}

type ServiceImpl struct {
	repo   Repository
	logger *zap.SugaredLogger
}

// MARK: Constructor
func NewService(repo Repository, logger *zap.SugaredLogger) Service {
	return &ServiceImpl{
		repo:   repo,
		logger: logger.Named("[api-key-service]"),
	}
}

// MARK: Create
func (s *ServiceImpl) Create(ctx context.Context, req *CreateRequest) (*APIKeyWithToken, error) {
	s.logger.Infow("Creating API key", "name", req.Name)

	// Validate expiration date
	if req.ExpiresAt != nil && req.ExpiresAt.Before(time.Now()) {
		s.logger.Warnw("Invalid expiration date provided", "expiresAt", req.ExpiresAt)
		return nil, errors.New("expiration date cannot be in the past")
	}

	// Validate max usage count
	if req.MaxUsageCount != nil && *req.MaxUsageCount <= 0 {
		s.logger.Warnw("Invalid max usage count provided", "maxUsageCount", *req.MaxUsageCount)
		return nil, errors.New("max usage count must be greater than 0")
	}

	createModel := &CreateModel{
		Name:          req.Name,
		ExpiresAt:     req.ExpiresAt,
		MaxUsageCount: req.MaxUsageCount,
	}

	result, err := s.repo.Create(ctx, createModel)
	if err != nil {
		s.logger.Errorw("Failed to create API key", "name", req.Name, "error", err)
		return nil, err
	}

	s.logger.Infow("API key created successfully", "apiKeyId", result.ID, "name", req.Name)
	return result, nil
}

// MARK: FindAll
func (s *ServiceImpl) FindAll(ctx context.Context) ([]*Model, error) {
	return s.repo.FindAll(ctx)
}

// MARK: FindByID
func (s *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return s.repo.FindByID(ctx, id)
}

// MARK: Update
func (s *ServiceImpl) Update(ctx context.Context, id string, req *UpdateRequest) (*Model, error) {
	// First, verify the API key exists
	apiKey, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if apiKey == nil {
		return nil, errors.New("API key not found")
	}

	// Validate expiration date if provided
	if req.ExpiresAt != nil {
		if req.ExpiresAt.Before(time.Now()) {
			return nil, errors.New("expiration date cannot be in the past")
		}
	}

	// Validate max usage count if provided
	if req.MaxUsageCount != nil {
		if *req.MaxUsageCount <= 0 {
			return nil, errors.New("max usage count must be greater than 0")
		}
	}

	updateModel := &UpdateModel{
		Name:          req.Name,
		ExpiresAt:     req.ExpiresAt,
		MaxUsageCount: req.MaxUsageCount,
	}

	return s.repo.Update(ctx, id, updateModel)
}

// MARK: Delete
func (s *ServiceImpl) Delete(ctx context.Context, id string) error {
	// First, verify the API key exists
	apiKey, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if apiKey == nil {
		s.logger.Warnw("API key not found", "id", id)
		return errors.New("API key not found")
	}

	return s.repo.Delete(ctx, id)
}

// MARK: ValidateKey
func (s *ServiceImpl) ValidateKey(ctx context.Context, key string) (*Model, error) {
	s.logger.Debugw("Validating API key", "key", maskAPIKey(key))

	// Validate key format
	if !isValidAPIKeyFormat(key) {
		s.logger.Warnw("Invalid API key format", "key", maskAPIKey(key))
		return nil, errors.New("Invalid API key")
	}

	// Get all API keys and compare using bcrypt.CompareHashAndPassword
	// This is necessary because bcrypt generates different hashes each time
	// We need to iterate through all keys and compare the plain text
	apiKeys, err := s.repo.FindAll(ctx)
	if err != nil {
		s.logger.Errorw("Error finding API keys", "key", maskAPIKey(key), "error", err)
		return nil, err
	}

	// Find matching API key by comparing with stored hash
	var matchedKey *Model
	for _, apiKey := range apiKeys {
		err := bcrypt.CompareHashAndPassword([]byte(apiKey.KeyHash), []byte(key))
		if err == nil {
			matchedKey = apiKey
			break
		}
	}

	if matchedKey == nil {
		s.logger.Warnw("API key not found", "key", maskAPIKey(key))
		return nil, errors.New("Invalid API key")
	}

	// Check if the key has expired
	if matchedKey.ExpiresAt != nil && matchedKey.ExpiresAt.Before(time.Now()) {
		s.logger.Warnw("API key has expired", "apiKeyId", matchedKey.ID, "expiresAt", matchedKey.ExpiresAt)
		return nil, errors.New("API key has expired")
	}

	// Check if the key has exceeded max usage count
	if matchedKey.MaxUsageCount != nil && matchedKey.UsageCount >= *matchedKey.MaxUsageCount {
		s.logger.Warnw("API key usage limit exceeded", "apiKeyId", matchedKey.ID, "usageCount", matchedKey.UsageCount, "maxUsageCount", *matchedKey.MaxUsageCount)
		return nil, errors.New("API key usage limit exceeded")
	}

	// Update last used timestamp and usage count
	err = s.repo.UpdateLastUsed(ctx, matchedKey.ID)
	if err != nil {
		// Log error but don't fail the validation
		// This is a non-critical operation
		s.logger.Errorw("Error updating last used timestamp and usage count", "apiKeyId", matchedKey.ID, "error", err)
	}

	s.logger.Infow("API key validation successful", "apiKeyId", matchedKey.ID, "name", matchedKey.Name)
	return matchedKey, nil
}

