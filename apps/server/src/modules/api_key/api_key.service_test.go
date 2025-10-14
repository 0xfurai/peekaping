package api_key

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"go.uber.org/zap"
)

func setupServiceTestDB(t *testing.T) *bun.DB {
	sqldb, err := sql.Open(sqliteshim.ShimName, ":memory:")
	require.NoError(t, err)

	db := bun.NewDB(sqldb, sqlitedialect.New())

	// Create tables
	_, err = db.NewCreateTable().Model((*sqlModel)(nil)).Exec(context.Background())
	require.NoError(t, err)

	return db
}

func TestServiceImpl_ValidateKey_Success(t *testing.T) {
	db := setupServiceTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()

	// Create an API key first
	req := &CreateRequest{
		Name:          "Test Key",
		ExpiresAt:     nil,
		MaxUsageCount: nil,
	}

	result, err := service.Create(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Now validate the created key
	validated, err := service.ValidateKey(ctx, result.Token)
	assert.NoError(t, err)
	assert.NotNil(t, validated)
	assert.Equal(t, result.ID, validated.ID)
	assert.Equal(t, result.Name, validated.Name)

	// Verify usage count was incremented
	updatedKey, err := service.FindByID(ctx, result.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), updatedKey.UsageCount)
	assert.NotNil(t, updatedKey.LastUsed)
}

func TestServiceImpl_ValidateKey_ExpiredKey(t *testing.T) {
	db := setupServiceTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()

	// Create an API key with expiration in the past
	expiredTime := time.Now().Add(-1 * time.Hour)
	req := &CreateRequest{
		Name:          "Expired Key",
		ExpiresAt:     &expiredTime,
		MaxUsageCount: nil,
	}

	result, err := service.Create(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Try to validate the expired key
	validated, err := service.ValidateKey(ctx, result.Token)
	assert.Error(t, err)
	assert.Nil(t, validated)
	assert.Contains(t, err.Error(), "expired")
}

func TestServiceImpl_ValidateKey_MaxUsageExceeded(t *testing.T) {
	db := setupServiceTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()

	// Create an API key with usage limit
	maxUsage := int64(2)
	req := &CreateRequest{
		Name:          "Limited Key",
		ExpiresAt:     nil,
		MaxUsageCount: &maxUsage,
	}

	result, err := service.Create(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	// First validation should succeed
	_, err = service.ValidateKey(ctx, result.Token)
	assert.NoError(t, err)

	// Second validation should succeed
	_, err = service.ValidateKey(ctx, result.Token)
	assert.NoError(t, err)

	// Third validation should fail (exceeded limit)
	validated, err := service.ValidateKey(ctx, result.Token)
	assert.Error(t, err)
	assert.Nil(t, validated)
	assert.Contains(t, err.Error(), "usage limit exceeded")
}

func TestServiceImpl_ValidateKey_InvalidFormat(t *testing.T) {
	db := setupServiceTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()
	invalidToken := "invalid-token-format"

	result, err := service.ValidateKey(ctx, invalidToken)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid API key")
}

func TestServiceImpl_ValidateKey_InvalidHash(t *testing.T) {
	db := setupServiceTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()

	// Create a valid API key
	req := &CreateRequest{
		Name:          "Test Key",
		ExpiresAt:     nil,
		MaxUsageCount: nil,
	}

	result, err := service.Create(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Create a fake token with the same ID but different key
	fakeToken := ApiKeyPrefix + "eyJpZCI6InRlc3Qta2V5LWlkIiwia2V5Ijoid3Jvbmcta2V5In0="

	// Try to validate with wrong key
	validated, err := service.ValidateKey(ctx, fakeToken)
	assert.Error(t, err)
	assert.Nil(t, validated)
	assert.Contains(t, err.Error(), "invalid API key")
}

func TestServiceImpl_ValidateKey_NotFound(t *testing.T) {
	db := setupServiceTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()
	nonExistentToken := ApiKeyPrefix + "eyJpZCI6Im5vbi1leGlzdGVudC1pZCIsImtleSI6InRlc3QtYWN0dWFsLWtleSJ9"

	result, err := service.ValidateKey(ctx, nonExistentToken)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid API key")
}

func TestServiceImpl_Create_Success(t *testing.T) {
	db := setupServiceTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()
	req := &CreateRequest{
		Name:          "Test API Key",
		ExpiresAt:     nil,
		MaxUsageCount: nil,
	}

	result, err := service.Create(ctx, req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, req.Name, result.Name)
	assert.NotEmpty(t, result.Token)
	assert.True(t, len(result.Token) > len(ApiKeyPrefix))
	assert.True(t, len(result.KeyHash) > 0)
	assert.True(t, len(result.DisplayKey) > 0)
	assert.Equal(t, int64(0), result.UsageCount)
	assert.Nil(t, result.LastUsed)
}

func TestServiceImpl_FindAll(t *testing.T) {
	db := setupServiceTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()

	// Create multiple API keys
	req1 := &CreateRequest{Name: "Key 1"}
	req2 := &CreateRequest{Name: "Key 2"}

	_, err := service.Create(ctx, req1)
	require.NoError(t, err)
	_, err = service.Create(ctx, req2)
	require.NoError(t, err)

	result, err := service.FindAll(ctx)
	assert.NoError(t, err)
	assert.Len(t, result, 2)

	// Verify both keys exist
	names := make([]string, len(result))
	for i, key := range result {
		names[i] = key.Name
	}
	assert.Contains(t, names, "Key 1")
	assert.Contains(t, names, "Key 2")
}

func TestServiceImpl_FindByID(t *testing.T) {
	db := setupServiceTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()

	// Create an API key
	req := &CreateRequest{Name: "Test Key"}
	created, err := service.Create(ctx, req)
	require.NoError(t, err)

	// Find it by ID
	result, err := service.FindByID(ctx, created.ID)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, created.ID, result.ID)
	assert.Equal(t, "Test Key", result.Name)
}

func TestServiceImpl_Update(t *testing.T) {
	db := setupServiceTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()

	// Create an API key
	req := &CreateRequest{Name: "Original Name"}
	created, err := service.Create(ctx, req)
	require.NoError(t, err)

	// Update it
	updateReq := &UpdateRequest{
		Name: stringPtr("Updated Name"),
	}

	result, err := service.Update(ctx, created.ID, updateReq)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Updated Name", result.Name)
	assert.Equal(t, created.ID, result.ID)
}

func TestServiceImpl_Delete(t *testing.T) {
	db := setupServiceTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()

	// Create an API key
	req := &CreateRequest{Name: "Test Key"}
	created, err := service.Create(ctx, req)
	require.NoError(t, err)

	// Verify it exists
	_, err = service.FindByID(ctx, created.ID)
	assert.NoError(t, err)

	// Delete it
	err = service.Delete(ctx, created.ID)
	assert.NoError(t, err)

	// Verify it's gone
	deletedKey, err := service.FindByID(ctx, created.ID)
	assert.NoError(t, err)
	assert.Nil(t, deletedKey)
}

// Helper function for string pointers
func stringPtr(s string) *string {
	return &s
}
