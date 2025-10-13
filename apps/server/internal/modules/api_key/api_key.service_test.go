package api_key

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// Mock Repository
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) Create(ctx context.Context, apiKey *CreateModel) (*APIKeyWithToken, error) {
	args := m.Called(ctx, apiKey)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*APIKeyWithToken), args.Error(1)
}

func (m *MockRepository) FindAll(ctx context.Context) ([]*Model, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*Model), args.Error(1)
}

func (m *MockRepository) FindByID(ctx context.Context, id string) (*Model, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) Update(ctx context.Context, id string, update *UpdateModel) (*Model, error) {
	args := m.Called(ctx, id, update)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockRepository) UpdateLastUsed(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockRepository) UpdateKeyHash(ctx context.Context, id string, keyHash string, displayKey string) error {
	args := m.Called(ctx, id, keyHash, displayKey)
	return args.Error(0)
}

// Test setup helper
func setupAPIKeyService() (*ServiceImpl, *MockRepository) {
	mockRepo := &MockRepository{}
	logger := zap.NewNop().Sugar()
	service := NewService(mockRepo, logger).(*ServiceImpl)
	return service, mockRepo
}

// MARK: Create Tests

func TestAPIKeyService_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("successful creation", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		req := &CreateRequest{
			Name:          "Test API Key",
			ExpiresAt:     nil,
			MaxUsageCount: nil,
		}

		// Mock the initial create to return an API key with ID
		createdKey := &APIKeyWithToken{
			Model: Model{
				ID:        "test-id-123",
				Name:      req.Name,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		}

		mockRepo.On("Create", ctx, mock.MatchedBy(func(cm *CreateModel) bool {
			return cm.Name == req.Name
		})).Return(createdKey, nil)

		// Mock the UpdateKeyHash call
		mockRepo.On("UpdateKeyHash", ctx, "test-id-123", mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil)

		result, err := service.Create(ctx, req)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "test-id-123", result.ID)
		assert.Equal(t, req.Name, result.Name)
		assert.NotEmpty(t, result.Token)
		assert.NotEmpty(t, result.KeyHash)
		assert.NotEmpty(t, result.DisplayKey)
		assert.True(t, isValidAPIKeyFormat(result.Token))
		mockRepo.AssertExpectations(t)
	})

	t.Run("creation with expiry date", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		expiresAt := time.Now().Add(30 * 24 * time.Hour)
		req := &CreateRequest{
			Name:      "Expiring Key",
			ExpiresAt: &expiresAt,
		}

		createdKey := &APIKeyWithToken{
			Model: Model{
				ID:        "test-id-456",
				Name:      req.Name,
				ExpiresAt: req.ExpiresAt,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		}

		mockRepo.On("Create", ctx, mock.MatchedBy(func(cm *CreateModel) bool {
			return cm.Name == req.Name && cm.ExpiresAt != nil
		})).Return(createdKey, nil)

		mockRepo.On("UpdateKeyHash", ctx, "test-id-456", mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil)

		result, err := service.Create(ctx, req)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, req.ExpiresAt, result.ExpiresAt)
		mockRepo.AssertExpectations(t)
	})

	t.Run("creation with max usage count", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		maxUsage := int64(1000)
		req := &CreateRequest{
			Name:          "Limited Usage Key",
			MaxUsageCount: &maxUsage,
		}

		createdKey := &APIKeyWithToken{
			Model: Model{
				ID:            "test-id-789",
				Name:          req.Name,
				MaxUsageCount: req.MaxUsageCount,
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
			},
		}

		mockRepo.On("Create", ctx, mock.MatchedBy(func(cm *CreateModel) bool {
			return cm.Name == req.Name && cm.MaxUsageCount != nil && *cm.MaxUsageCount == maxUsage
		})).Return(createdKey, nil)

		mockRepo.On("UpdateKeyHash", ctx, "test-id-789", mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil)

		result, err := service.Create(ctx, req)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, req.MaxUsageCount, result.MaxUsageCount)
		mockRepo.AssertExpectations(t)
	})

	t.Run("repository create error", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		req := &CreateRequest{
			Name: "Test Key",
		}

		mockRepo.On("Create", ctx, mock.Anything).Return(nil, errors.New("database error"))

		result, err := service.Create(ctx, req)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "database error")
		mockRepo.AssertExpectations(t)
	})

	t.Run("update key hash error with cleanup", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		req := &CreateRequest{
			Name: "Test Key",
		}

		createdKey := &APIKeyWithToken{
			Model: Model{
				ID:        "test-id-cleanup",
				Name:      req.Name,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		}

		mockRepo.On("Create", ctx, mock.Anything).Return(createdKey, nil)
		mockRepo.On("UpdateKeyHash", ctx, "test-id-cleanup", mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(errors.New("update hash error"))
		mockRepo.On("Delete", ctx, "test-id-cleanup").Return(nil)

		result, err := service.Create(ctx, req)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "update hash error")
		mockRepo.AssertExpectations(t)
	})
}

// MARK: FindAll Tests

func TestAPIKeyService_FindAll(t *testing.T) {
	ctx := context.Background()

	t.Run("successful find all", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		expectedKeys := []*Model{
			{
				ID:        "key1",
				Name:      "Key 1",
				CreatedAt: time.Now(),
			},
			{
				ID:        "key2",
				Name:      "Key 2",
				CreatedAt: time.Now(),
			},
		}

		mockRepo.On("FindAll", ctx).Return(expectedKeys, nil)

		result, err := service.FindAll(ctx)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Len(t, result, 2)
		assert.Equal(t, expectedKeys, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("empty result", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		expectedKeys := []*Model{}

		mockRepo.On("FindAll", ctx).Return(expectedKeys, nil)

		result, err := service.FindAll(ctx)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Len(t, result, 0)
		mockRepo.AssertExpectations(t)
	})

	t.Run("repository error", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		mockRepo.On("FindAll", ctx).Return(nil, errors.New("database error"))

		result, err := service.FindAll(ctx)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "database error")
		mockRepo.AssertExpectations(t)
	})
}

// MARK: FindByID Tests

func TestAPIKeyService_FindByID(t *testing.T) {
	ctx := context.Background()

	t.Run("successful find by ID", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "test-key-123"
		expectedKey := &Model{
			ID:        keyID,
			Name:      "Test Key",
			CreatedAt: time.Now(),
		}

		mockRepo.On("FindByID", ctx, keyID).Return(expectedKey, nil)

		result, err := service.FindByID(ctx, keyID)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, expectedKey, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("key not found", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "non-existent"

		mockRepo.On("FindByID", ctx, keyID).Return(nil, errors.New("not found"))

		result, err := service.FindByID(ctx, keyID)

		assert.Error(t, err)
		assert.Nil(t, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("repository error", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "test-key-123"

		mockRepo.On("FindByID", ctx, keyID).Return(nil, errors.New("database error"))

		result, err := service.FindByID(ctx, keyID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "database error")
		mockRepo.AssertExpectations(t)
	})
}

// MARK: Update Tests

func TestAPIKeyService_Update(t *testing.T) {
	ctx := context.Background()

	t.Run("successful update", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "test-key-123"
		newName := "Updated Key Name"
		req := &UpdateRequest{
			Name: &newName,
		}

		existingKey := &Model{
			ID:   keyID,
			Name: "Old Name",
		}

		updatedKey := &Model{
			ID:   keyID,
			Name: newName,
		}

		mockRepo.On("FindByID", ctx, keyID).Return(existingKey, nil)
		mockRepo.On("Update", ctx, keyID, mock.MatchedBy(func(um *UpdateModel) bool {
			return um.Name != nil && *um.Name == newName
		})).Return(updatedKey, nil)

		result, err := service.Update(ctx, keyID, req)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, newName, result.Name)
		mockRepo.AssertExpectations(t)
	})

	t.Run("update with expiry date", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "test-key-123"
		newExpiry := time.Now().Add(60 * 24 * time.Hour)
		req := &UpdateRequest{
			ExpiresAt: &newExpiry,
		}

		existingKey := &Model{
			ID:   keyID,
			Name: "Test Key",
		}

		updatedKey := &Model{
			ID:        keyID,
			Name:      "Test Key",
			ExpiresAt: &newExpiry,
		}

		mockRepo.On("FindByID", ctx, keyID).Return(existingKey, nil)
		mockRepo.On("Update", ctx, keyID, mock.MatchedBy(func(um *UpdateModel) bool {
			return um.ExpiresAt != nil
		})).Return(updatedKey, nil)

		result, err := service.Update(ctx, keyID, req)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, &newExpiry, result.ExpiresAt)
		mockRepo.AssertExpectations(t)
	})

	t.Run("update with max usage count", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "test-key-123"
		newMaxUsage := int64(5000)
		req := &UpdateRequest{
			MaxUsageCount: &newMaxUsage,
		}

		existingKey := &Model{
			ID:   keyID,
			Name: "Test Key",
		}

		updatedKey := &Model{
			ID:            keyID,
			Name:          "Test Key",
			MaxUsageCount: &newMaxUsage,
		}

		mockRepo.On("FindByID", ctx, keyID).Return(existingKey, nil)
		mockRepo.On("Update", ctx, keyID, mock.MatchedBy(func(um *UpdateModel) bool {
			return um.MaxUsageCount != nil && *um.MaxUsageCount == newMaxUsage
		})).Return(updatedKey, nil)

		result, err := service.Update(ctx, keyID, req)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, &newMaxUsage, result.MaxUsageCount)
		mockRepo.AssertExpectations(t)
	})

	t.Run("key not found", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "non-existent"
		newName := "Updated Name"
		req := &UpdateRequest{
			Name: &newName,
		}

		mockRepo.On("FindByID", ctx, keyID).Return(nil, errors.New("not found"))

		result, err := service.Update(ctx, keyID, req)

		assert.Error(t, err)
		assert.Nil(t, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("key is nil", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "test-key-123"
		newName := "Updated Name"
		req := &UpdateRequest{
			Name: &newName,
		}

		mockRepo.On("FindByID", ctx, keyID).Return(nil, nil)

		result, err := service.Update(ctx, keyID, req)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "API key not found")
		mockRepo.AssertExpectations(t)
	})

	t.Run("repository update error", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "test-key-123"
		newName := "Updated Name"
		req := &UpdateRequest{
			Name: &newName,
		}

		existingKey := &Model{
			ID:   keyID,
			Name: "Old Name",
		}

		mockRepo.On("FindByID", ctx, keyID).Return(existingKey, nil)
		mockRepo.On("Update", ctx, keyID, mock.Anything).Return(nil, errors.New("update failed"))

		result, err := service.Update(ctx, keyID, req)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "update failed")
		mockRepo.AssertExpectations(t)
	})
}

// MARK: Delete Tests

func TestAPIKeyService_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("successful delete", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "test-key-123"

		existingKey := &Model{
			ID:   keyID,
			Name: "Test Key",
		}

		mockRepo.On("FindByID", ctx, keyID).Return(existingKey, nil)
		mockRepo.On("Delete", ctx, keyID).Return(nil)

		err := service.Delete(ctx, keyID)

		assert.NoError(t, err)
		mockRepo.AssertExpectations(t)
	})

	t.Run("key not found but still deletes", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "non-existent"

		mockRepo.On("FindByID", ctx, keyID).Return(nil, nil)
		mockRepo.On("Delete", ctx, keyID).Return(nil)

		err := service.Delete(ctx, keyID)

		assert.NoError(t, err)
		mockRepo.AssertExpectations(t)
	})

	t.Run("repository find error", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "test-key-123"

		mockRepo.On("FindByID", ctx, keyID).Return(nil, errors.New("database error"))

		err := service.Delete(ctx, keyID)

		assert.Error(t, err) // Returns error from FindByID
		assert.Contains(t, err.Error(), "database error")
		mockRepo.AssertExpectations(t)
	})

	t.Run("repository delete error", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "test-key-123"

		existingKey := &Model{
			ID:   keyID,
			Name: "Test Key",
		}

		mockRepo.On("FindByID", ctx, keyID).Return(existingKey, nil)
		mockRepo.On("Delete", ctx, keyID).Return(errors.New("delete failed"))

		err := service.Delete(ctx, keyID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "delete failed")
		mockRepo.AssertExpectations(t)
	})
}

// MARK: ValidateKey Tests

func TestAPIKeyService_ValidateKey(t *testing.T) {
	ctx := context.Background()

	t.Run("successful validation", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		// First, create a valid API key with proper token
		keyID := "test-key-123"

		// Generate a valid token
		token, _, _, err := service.generateAPIKey(keyID)
		assert.NoError(t, err)

		// Parse the token to get the actual key from it
		parsedID, parsedKey, err := service.parseAPIKeyToken(token)
		assert.NoError(t, err)
		assert.Equal(t, keyID, parsedID)

		// Create the stored key with the hash of the parsed key
		hashedParsedKey, err := bcrypt.GenerateFromPassword([]byte(parsedKey), bcrypt.DefaultCost)
		assert.NoError(t, err)

		storedKey := &Model{
			ID:         keyID,
			Name:       "Test Key",
			KeyHash:    string(hashedParsedKey),
			UsageCount: 5,
			CreatedAt:  time.Now(),
		}

		mockRepo.On("FindByID", ctx, keyID).Return(storedKey, nil)
		mockRepo.On("UpdateLastUsed", ctx, keyID).Return(nil)

		result, err := service.ValidateKey(ctx, token)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, keyID, result.ID)
		mockRepo.AssertExpectations(t)
	})

	t.Run("invalid token format", func(t *testing.T) {
		service, _ := setupAPIKeyService()

		invalidToken := "invalid-token"

		result, err := service.ValidateKey(ctx, invalidToken)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "Invalid API key")
	})

	t.Run("key not found", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		// Generate a valid token
		keyID := "non-existent-key"
		token, _, _, err := service.generateAPIKey(keyID)
		assert.NoError(t, err)

		mockRepo.On("FindByID", ctx, keyID).Return(nil, errors.New("not found"))

		result, err := service.ValidateKey(ctx, token)

		assert.Error(t, err)
		assert.Nil(t, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("key is nil", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "test-key-123"
		token, _, _, err := service.generateAPIKey(keyID)
		assert.NoError(t, err)

		mockRepo.On("FindByID", ctx, keyID).Return(nil, nil)

		result, err := service.ValidateKey(ctx, token)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "Invalid API key")
		mockRepo.AssertExpectations(t)
	})

	t.Run("expired key", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "test-key-123"
		token, _, _, err := service.generateAPIKey(keyID)
		assert.NoError(t, err)

		parsedID, parsedKey, err := service.parseAPIKeyToken(token)
		assert.NoError(t, err)
		assert.Equal(t, keyID, parsedID)

		hashedParsedKey, err := bcrypt.GenerateFromPassword([]byte(parsedKey), bcrypt.DefaultCost)
		assert.NoError(t, err)

		expiredTime := time.Now().Add(-24 * time.Hour)
		storedKey := &Model{
			ID:        keyID,
			Name:      "Expired Key",
			KeyHash:   string(hashedParsedKey),
			ExpiresAt: &expiredTime,
		}

		mockRepo.On("FindByID", ctx, keyID).Return(storedKey, nil)

		result, err := service.ValidateKey(ctx, token)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "API key has expired")
		mockRepo.AssertExpectations(t)
	})

	t.Run("usage limit exceeded", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "test-key-123"
		token, _, _, err := service.generateAPIKey(keyID)
		assert.NoError(t, err)

		parsedID, parsedKey, err := service.parseAPIKeyToken(token)
		assert.NoError(t, err)
		assert.Equal(t, keyID, parsedID)

		hashedParsedKey, err := bcrypt.GenerateFromPassword([]byte(parsedKey), bcrypt.DefaultCost)
		assert.NoError(t, err)

		maxUsage := int64(100)
		storedKey := &Model{
			ID:            keyID,
			Name:          "Limited Key",
			KeyHash:       string(hashedParsedKey),
			UsageCount:    100,
			MaxUsageCount: &maxUsage,
		}

		mockRepo.On("FindByID", ctx, keyID).Return(storedKey, nil)

		result, err := service.ValidateKey(ctx, token)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "usage limit exceeded")
		mockRepo.AssertExpectations(t)
	})

	t.Run("invalid key hash", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "test-key-123"
		token, _, _, err := service.generateAPIKey(keyID)
		assert.NoError(t, err)

		// Use a different key for hashing
		wrongKey := "wrong-key-12345678901234567890"
		hashedWrongKey, err := bcrypt.GenerateFromPassword([]byte(wrongKey), bcrypt.DefaultCost)
		assert.NoError(t, err)

		storedKey := &Model{
			ID:      keyID,
			Name:    "Test Key",
			KeyHash: string(hashedWrongKey),
		}

		mockRepo.On("FindByID", ctx, keyID).Return(storedKey, nil)

		result, err := service.ValidateKey(ctx, token)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "Invalid API key")
		mockRepo.AssertExpectations(t)
	})

	t.Run("update last used error is logged but not failed", func(t *testing.T) {
		service, mockRepo := setupAPIKeyService()

		keyID := "test-key-123"
		token, _, _, err := service.generateAPIKey(keyID)
		assert.NoError(t, err)

		parsedID, parsedKey, err := service.parseAPIKeyToken(token)
		assert.NoError(t, err)
		assert.Equal(t, keyID, parsedID)

		hashedParsedKey, err := bcrypt.GenerateFromPassword([]byte(parsedKey), bcrypt.DefaultCost)
		assert.NoError(t, err)

		storedKey := &Model{
			ID:      keyID,
			Name:    "Test Key",
			KeyHash: string(hashedParsedKey),
		}

		mockRepo.On("FindByID", ctx, keyID).Return(storedKey, nil)
		mockRepo.On("UpdateLastUsed", ctx, keyID).Return(errors.New("update failed"))

		result, err := service.ValidateKey(ctx, token)

		// Should still succeed despite UpdateLastUsed error
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, keyID, result.ID)
		mockRepo.AssertExpectations(t)
	})
}

// MARK: Helper Method Tests

func TestAPIKeyService_generateAPIKey(t *testing.T) {
	service, _ := setupAPIKeyService()

	t.Run("generates valid API key", func(t *testing.T) {
		apiKeyID := "test-id-123"

		token, keyHash, displayKey, err := service.generateAPIKey(apiKeyID)

		assert.NoError(t, err)
		assert.NotEmpty(t, token)
		assert.NotEmpty(t, keyHash)
		assert.NotEmpty(t, displayKey)
		assert.True(t, isValidAPIKeyFormat(token))
		assert.Contains(t, token, ApiKeyPrefix)

		// Verify the display key is masked
		assert.Contains(t, displayKey, "...")

		// Verify the token can be parsed
		parsedID, parsedKey, err := service.parseAPIKeyToken(token)
		assert.NoError(t, err)
		assert.Equal(t, apiKeyID, parsedID)
		assert.NotEmpty(t, parsedKey)

		// Verify the hash is valid bcrypt hash
		err = bcrypt.CompareHashAndPassword([]byte(keyHash), []byte(parsedKey))
		assert.NoError(t, err)
	})

	t.Run("generates unique keys", func(t *testing.T) {
		apiKeyID := "test-id-456"

		token1, hash1, _, err1 := service.generateAPIKey(apiKeyID)
		token2, hash2, _, err2 := service.generateAPIKey(apiKeyID)

		assert.NoError(t, err1)
		assert.NoError(t, err2)
		assert.NotEqual(t, token1, token2)
		assert.NotEqual(t, hash1, hash2)
	})
}

func TestAPIKeyService_parseAPIKeyToken(t *testing.T) {
	service, _ := setupAPIKeyService()

	t.Run("successfully parses valid token", func(t *testing.T) {
		apiKeyID := "test-id-789"
		token, _, _, err := service.generateAPIKey(apiKeyID)
		assert.NoError(t, err)

		parsedID, parsedKey, err := service.parseAPIKeyToken(token)

		assert.NoError(t, err)
		assert.Equal(t, apiKeyID, parsedID)
		assert.NotEmpty(t, parsedKey)
	})

	t.Run("fails on invalid format", func(t *testing.T) {
		invalidToken := "invalid-token-without-prefix"

		parsedID, parsedKey, err := service.parseAPIKeyToken(invalidToken)

		assert.Error(t, err)
		assert.Empty(t, parsedID)
		assert.Empty(t, parsedKey)
		assert.Contains(t, err.Error(), "invalid API key format")
	})

	t.Run("fails on invalid base64", func(t *testing.T) {
		invalidToken := ApiKeyPrefix + "invalid-base64-!@#$%"

		parsedID, parsedKey, err := service.parseAPIKeyToken(invalidToken)

		assert.Error(t, err)
		assert.Empty(t, parsedID)
		assert.Empty(t, parsedKey)
	})

	t.Run("fails on invalid JSON payload", func(t *testing.T) {
		invalidToken := ApiKeyPrefix + "aW52YWxpZC1qc29u" // base64 of "invalid-json"

		parsedID, parsedKey, err := service.parseAPIKeyToken(invalidToken)

		assert.Error(t, err)
		assert.Empty(t, parsedID)
		assert.Empty(t, parsedKey)
	})
}

func TestAPIKeyService_hashAPIKey(t *testing.T) {
	service, _ := setupAPIKeyService()

	t.Run("successfully hashes key", func(t *testing.T) {
		key := "test-key-to-hash"

		hash, err := service.hashAPIKey(key)

		assert.NoError(t, err)
		assert.NotEmpty(t, hash)

		// Verify it's a valid bcrypt hash
		err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(key))
		assert.NoError(t, err)
	})

	t.Run("generates different hashes for same key", func(t *testing.T) {
		key := "test-key"

		hash1, err1 := service.hashAPIKey(key)
		hash2, err2 := service.hashAPIKey(key)

		assert.NoError(t, err1)
		assert.NoError(t, err2)
		assert.NotEqual(t, hash1, hash2) // bcrypt generates different salts

		// But both should validate correctly
		err := bcrypt.CompareHashAndPassword([]byte(hash1), []byte(key))
		assert.NoError(t, err)
		err = bcrypt.CompareHashAndPassword([]byte(hash2), []byte(key))
		assert.NoError(t, err)
	})
}

// MARK: Constructor Test

func TestNewService(t *testing.T) {
	mockRepo := &MockRepository{}
	logger := zap.NewNop().Sugar()

	service := NewService(mockRepo, logger)

	assert.NotNil(t, service)
	assert.IsType(t, &ServiceImpl{}, service)

	serviceImpl := service.(*ServiceImpl)
	assert.Equal(t, mockRepo, serviceImpl.repo)
	assert.NotNil(t, serviceImpl.logger)
}
