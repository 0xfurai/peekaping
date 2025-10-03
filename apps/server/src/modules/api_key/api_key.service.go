package api_key

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Service defines the interface for API key business logic
type Service interface {
	Create(ctx context.Context, userID string, req *CreateRequest) (*APIKeyWithToken, error)
	FindByUserID(ctx context.Context, userID string) ([]*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	Update(ctx context.Context, id string, userID string, req *UpdateRequest) (*Model, error)
	Delete(ctx context.Context, id string, userID string) error
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
	ExpiresAt     **time.Time `json:"expires_at,omitempty"`
	MaxUsageCount **int64     `json:"max_usage_count,omitempty"`
}

type ServiceImpl struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &ServiceImpl{
		repo: repo,
	}
}

func (s *ServiceImpl) Create(ctx context.Context, userID string, req *CreateRequest) (*APIKeyWithToken, error) {
	// Validate expiration date
	if req.ExpiresAt != nil && req.ExpiresAt.Before(time.Now()) {
		return nil, errors.New("expiration date cannot be in the past")
	}

	// Validate max usage count
	if req.MaxUsageCount != nil && *req.MaxUsageCount <= 0 {
		return nil, errors.New("max usage count must be greater than 0")
	}

	createModel := &CreateModel{
		UserID:        userID,
		Name:          req.Name,
		ExpiresAt:     req.ExpiresAt,
		MaxUsageCount: req.MaxUsageCount,
	}

	return s.repo.Create(ctx, createModel)
}

func (s *ServiceImpl) FindByUserID(ctx context.Context, userID string) ([]*Model, error) {
	return s.repo.FindByUserID(ctx, userID)
}

func (s *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *ServiceImpl) Update(ctx context.Context, id string, userID string, req *UpdateRequest) (*Model, error) {
	// First, verify the API key belongs to the user
	apiKey, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if apiKey == nil {
		return nil, errors.New("API key not found")
	}
	if apiKey.UserID != userID {
		return nil, errors.New("unauthorized")
	}

	// Validate expiration date if provided
	if req.ExpiresAt != nil && *req.ExpiresAt != nil {
		if (*req.ExpiresAt).Before(time.Now()) {
			return nil, errors.New("expiration date cannot be in the past")
		}
	}

	// Validate max usage count if provided
	if req.MaxUsageCount != nil && *req.MaxUsageCount != nil {
		if **req.MaxUsageCount <= 0 {
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

func (s *ServiceImpl) Delete(ctx context.Context, id string, userID string) error {
	// First, verify the API key belongs to the user
	apiKey, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if apiKey == nil {
		return errors.New("API key not found")
	}
	if apiKey.UserID != userID {
		return errors.New("unauthorized")
	}

	return s.repo.Delete(ctx, id)
}

func (s *ServiceImpl) ValidateKey(ctx context.Context, key string) (*Model, error) {
	// Validate key format
	if !isValidAPIKeyFormat(key) {
		return nil, errors.New("invalid API key format")
	}

	// Hash the provided key
	keyHash, err := hashAPIKey(key)
	if err != nil {
		return nil, err
	}

	// Find the API key by hash
	apiKey, err := s.repo.FindByKeyHash(ctx, keyHash)
	if err != nil {
		return nil, err
	}
	if apiKey == nil {
		return nil, errors.New("invalid API key")
	}

	// Check if the key has expired
	if apiKey.ExpiresAt != nil && apiKey.ExpiresAt.Before(time.Now()) {
		return nil, errors.New("API key has expired")
	}

	// Check if the key has exceeded max usage count
	if apiKey.MaxUsageCount != nil && apiKey.UsageCount >= *apiKey.MaxUsageCount {
		return nil, errors.New("API key usage limit exceeded")
	}

	// Update last used timestamp and usage count
	err = s.repo.UpdateLastUsed(ctx, apiKey.ID)
	if err != nil {
		// Log error but don't fail the validation
		// This is a non-critical operation
	}

	return apiKey, nil
}

// generateAPIKey generates a secure API key with pk_ prefix
func generateAPIKey() (string, string, error) {
	// Generate 32 random bytes
	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", "", err
	}

	// Encode to base64 and add prefix
	key := "pk_" + base64.URLEncoding.EncodeToString(bytes)

	// Hash the key for storage
	keyHash, err := hashAPIKey(key)
	if err != nil {
		return "", "", err
	}

	return key, keyHash, nil
}

// hashAPIKey hashes an API key using bcrypt
func hashAPIKey(key string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// isValidAPIKeyFormat validates the format of an API key
func isValidAPIKeyFormat(key string) bool {
	// Check if it starts with pk_ and has reasonable length
	return len(key) >= 10 && len(key) <= 100 && 
		   len(key) > 3 && key[:3] == "pk_"
}
