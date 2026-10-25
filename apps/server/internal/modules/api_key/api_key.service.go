package api_key

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"peekaping/internal/legacyid"
	"strings"
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
	Name          string
	ExpiresAt     *time.Time
	MaxUsageCount *int64
}

// UpdateRequest represents the request to update an API key
type UpdateRequest struct {
	Name          *string
	ExpiresAt     *time.Time
	MaxUsageCount *int64
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

	// Phase 1: Create record with placeholder values to get database ID
	createModel := &CreateModel{
		Name:          req.Name,
		KeyHash:       "", // Empty initially
		DisplayKey:    "", // Empty initially
		ExpiresAt:     req.ExpiresAt,
		MaxUsageCount: req.MaxUsageCount,
	}

	result, err := s.repo.Create(ctx, createModel)
	if err != nil {
		s.logger.Errorw("Failed to create API key record", "name", req.Name, "error", err)
		return nil, err
	}

	// Phase 2: Generate API key using the database-generated ID
	token, keyHash, displayKey, err := s.generateAPIKey(result.ID)
	if err != nil {
		s.logger.Errorw("Failed to generate API key", "apiKeyId", result.ID, "name", req.Name, "error", err)
		// Cleanup: delete the created record
		_ = s.repo.Delete(ctx, result.ID)
		return nil, err
	}

	// Phase 3: Update the record with the actual key hash and display key
	err = s.repo.UpdateKeyHash(ctx, result.ID, keyHash, displayKey)
	if err != nil {
		s.logger.Errorw("Failed to update API key hash", "apiKeyId", result.ID, "name", req.Name, "error", err)
		// Cleanup: delete the created record
		_ = s.repo.Delete(ctx, result.ID)
		return nil, err
	}

	// Update result with generated values
	result.KeyHash = keyHash
	result.DisplayKey = displayKey
	result.Token = token

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
	}

	return s.repo.Delete(ctx, id)
}

// MARK: ValidateKey
func (s *ServiceImpl) ValidateKey(ctx context.Context, key string) (*Model, error) {
	s.logger.Debugw("Validating API key", "key", maskAPIKey(key))

	// Parse the API key token to extract ID and actual key
	apiKeyID, actualKey, err := s.parseAPIKeyToken(key)
	if err != nil {
		s.logger.Warnw("Invalid API key format", "key", maskAPIKey(key), "error", err)
		return nil, errors.New("invalid API key")
	}

	// Find the API key by the ID in the token
	apiKey, err := s.findForToken(ctx, apiKeyID)
	if err != nil {
		s.logger.Errorw("Error finding API key by ID", "apiKeyId", apiKeyID, "error", err)
		return nil, err
	}
	if apiKey == nil {
		s.logger.Warnw("API key not found", "apiKeyId", apiKeyID)
		return nil, errors.New("invalid API key")
	}

	// Check if the key has expired
	if apiKey.ExpiresAt != nil && apiKey.ExpiresAt.Before(time.Now()) {
		s.logger.Warnw("API key has expired", "apiKeyId", apiKey.ID, "expiresAt", apiKey.ExpiresAt)
		return nil, errors.New("API key has expired")
	}

	// Verify the actual key against the stored hash
	if !s.verifyKeyHash(ctx, apiKey, actualKey) {
		s.logger.Warnw("API key verification failed", "apiKeyId", apiKeyID)
		return nil, errors.New("invalid API key")
	}

	// Check the usage limit and count the use in one step, so that concurrent
	// requests can't go past the limit
	consumed, err := s.repo.ConsumeUsage(ctx, apiKey.ID)
	if err != nil {
		s.logger.Errorw("Error updating last used timestamp and usage count", "apiKeyId", apiKey.ID, "error", err)
		if apiKey.MaxUsageCount != nil {
			// Without the count, the limit can't be enforced
			return nil, err
		}
		// Without a limit, the count is non-critical: don't fail the validation
	} else if !consumed {
		s.logger.Warnw("API key usage limit exceeded", "apiKeyId", apiKey.ID, "maxUsageCount", apiKey.MaxUsageCount)
		return nil, errors.New("API key usage limit exceeded")
	}

	s.logger.Infow("API key validation successful", "apiKeyId", apiKey.ID, "name", apiKey.Name)
	return apiKey, nil
}

// MARK: findForToken
// findForToken finds the key that a token's ID names. A MongoDB install that
// was imported into SQL ("bun db import-mongo") keeps its tokens, which carry
// the key's ObjectID, while the key's row has the UUID that ObjectID maps to.
// So a 24-hex ID that finds nothing is looked up again under its UUID. Only a
// token can name an ObjectID here, so the retry is limited to this lookup.
func (s *ServiceImpl) findForToken(ctx context.Context, id string) (*Model, error) {
	apiKey, err := s.repo.FindByID(ctx, id)
	if err == nil && apiKey != nil {
		return apiKey, nil
	}
	// The first lookup may have failed instead of finding nothing: PostgreSQL
	// rejects an ObjectID as a UUID.
	mapped, ok := legacyid.FromObjectID(id)
	if !ok {
		return apiKey, err
	}
	if found, mappedErr := s.repo.FindByID(ctx, mapped); mappedErr == nil && found != nil {
		return found, nil
	}
	return apiKey, err
}

// MARK: generateAPIKey
// generateAPIKey generates a secure API key with the new format: prefix + base64encode({id: api_key_id, key: actual_key})
func (s *ServiceImpl) generateAPIKey(apiKeyID string) (string, string, string, error) {
	s.logger.Debugw("Starting API key generation", "apiKeyId", apiKeyID)

	// Generate random bytes for the actual key
	bytes := make([]byte, ApiKeyRandomBytes)
	_, err := rand.Read(bytes)
	if err != nil {
		s.logger.Errorw("Failed to generate random bytes for API key", "apiKeyId", apiKeyID, "error", err)
		return "", "", "", fmt.Errorf("error generating API key: %v", err)
	}

	// Create the actual key (without prefix)
	actualKey := base64.URLEncoding.EncodeToString(bytes)
	s.logger.Debugw("Generated actual key", "apiKeyId", apiKeyID, "keyLength", len(actualKey))

	// Hash the key for storage
	keyHash := hashAPIKey(actualKey)

	// Create the payload to encode in the token
	payload := map[string]string{
		"id":  apiKeyID,
		"key": actualKey, // Store the actual key, not the hash
	}

	// Encode the payload as JSON
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		s.logger.Errorw("Failed to marshal API key payload", "apiKeyId", apiKeyID, "error", err)
		return "", "", "", fmt.Errorf("error marshaling payload: %v", err)
	}

	// Create the final API key token: prefix + base64encode(payload)
	token := ApiKeyPrefix + base64.URLEncoding.EncodeToString(payloadJSON)
	s.logger.Debugw("Generated API key token", "apiKeyId", apiKeyID, "tokenLength", len(token))

	// Generate display key (masked version)
	displayKey := maskAPIKey(token)
	s.logger.Debugw("Generated display key", "apiKeyId", apiKeyID, "displayKey", displayKey)

	s.logger.Infow("API key generation completed successfully", "apiKeyId", apiKeyID, "tokenLength", len(token))
	return token, keyHash, displayKey, nil
}

// MARK: parseAPIKeyToken
// parseAPIKeyToken parses an API key token and extracts the ID and actual key
func (s *ServiceImpl) parseAPIKeyToken(token string) (string, string, error) {
	s.logger.Debugw("Parsing API key token", "tokenLength", len(token))

	// Remove prefix
	if !isValidAPIKeyFormat(token) {
		s.logger.Warnw("Invalid API key format", "tokenLength", len(token))
		return "", "", fmt.Errorf("invalid API key format")
	}

	// Decode base64 payload
	payloadB64 := token[len(ApiKeyPrefix):]
	payloadJSON, err := base64.URLEncoding.DecodeString(payloadB64)
	if err != nil {
		s.logger.Warnw("Failed to decode API key payload", "tokenLength", len(token), "error", err)
		return "", "", fmt.Errorf("error decoding API key payload: %v", err)
	}

	// Parse JSON payload
	var payload map[string]string
	err = json.Unmarshal(payloadJSON, &payload)
	if err != nil {
		s.logger.Warnw("Failed to parse API key payload JSON", "tokenLength", len(token), "error", err)
		return "", "", fmt.Errorf("error parsing API key payload: %v", err)
	}

	// Extract ID and actual key
	apiKeyID, hasID := payload["id"]
	actualKey, hasKey := payload["key"]

	if !hasID || !hasKey {
		s.logger.Warnw("Invalid API key payload structure", "tokenLength", len(token), "hasID", hasID, "hasKey", hasKey)
		return "", "", fmt.Errorf("invalid API key payload structure")
	}

	s.logger.Debugw("Successfully parsed API key token", "apiKeyId", apiKeyID, "keyLength", len(actualKey))
	return apiKeyID, actualKey, nil
}

// MARK: hashAPIKey
// hashAPIKey hashes an API key with SHA-256, as "sha256$" and the hex digest. The
// key is 32 random bytes, so it needs no slow hash, and a fast one keeps the check
// on every request cheap.
func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return KeyHashPrefix + hex.EncodeToString(sum[:])
}

// MARK: verifyKeyHash
// verifyKeyHash reports whether key matches the hash stored for apiKey. A key
// created before SHA-256 was used is stored as a bcrypt hash, which starts with
// "$2": it is checked with bcrypt once, and its stored hash is replaced by a
// SHA-256 hash.
func (s *ServiceImpl) verifyKeyHash(ctx context.Context, apiKey *Model, key string) bool {
	switch {
	case strings.HasPrefix(apiKey.KeyHash, KeyHashPrefix):
		return subtle.ConstantTimeCompare([]byte(apiKey.KeyHash), []byte(hashAPIKey(key))) == 1
	case strings.HasPrefix(apiKey.KeyHash, "$2"):
		if bcrypt.CompareHashAndPassword([]byte(apiKey.KeyHash), []byte(key)) != nil {
			return false
		}
		if err := s.repo.UpdateKeyHash(ctx, apiKey.ID, hashAPIKey(key), apiKey.DisplayKey); err != nil {
			// Log error but don't fail the validation: the next use tries again
			s.logger.Errorw("Error replacing the bcrypt hash of an API key", "apiKeyId", apiKey.ID, "error", err)
		}
		return true
	default:
		return false
	}
}
