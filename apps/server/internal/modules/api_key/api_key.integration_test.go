package api_key

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"go.uber.org/zap"
)

func setupTestDB(t *testing.T) *bun.DB {
	sqldb, err := sql.Open(sqliteshim.ShimName, "file::memory:?cache=shared")
	require.NoError(t, err)

	db := bun.NewDB(sqldb, sqlitedialect.New())

	// Create api_keys table matching the schema
	_, err = db.Exec(`
		CREATE TABLE api_keys (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL,
			display_key TEXT NOT NULL,
			last_used DATETIME,
			expires_at DATETIME,
			usage_count INTEGER NOT NULL DEFAULT 0,
			max_usage_count INTEGER,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	require.NoError(t, err)

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

func TestIntegration_CreateAndValidateAPIKey(t *testing.T) {
	db := setupTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()

	// Create API key
	req := &CreateRequest{
		Name:          "Integration Test Key",
		ExpiresAt:     nil,
		MaxUsageCount: nil,
	}

	result, err := service.Create(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, req.Name, result.Name)
	assert.NotEmpty(t, result.Token)
	assert.True(t, len(result.KeyHash) > 0)
	assert.True(t, len(result.DisplayKey) > 0)

	// Validate the created key
	validated, err := service.ValidateKey(ctx, result.Token)
	require.NoError(t, err)
	assert.Equal(t, result.ID, validated.ID)
	assert.Equal(t, result.Name, validated.Name)

	// Fetch updated values from database to verify usage count was incremented
	updatedKey, err := service.FindByID(ctx, result.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), updatedKey.UsageCount)
	assert.NotNil(t, updatedKey.LastUsed)
}

func TestIntegration_ExpiredKeyValidation(t *testing.T) {
	db := setupTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()

	// Create API key with expiration in the past
	expiredTime := time.Now().Add(-1 * time.Hour)
	req := &CreateRequest{
		Name:          "Expired Test Key",
		ExpiresAt:     &expiredTime,
		MaxUsageCount: nil,
	}

	result, err := service.Create(ctx, req)
	require.NoError(t, err)

	// Try to validate expired key
	validated, err := service.ValidateKey(ctx, result.Token)
	assert.Error(t, err)
	assert.Nil(t, validated)
	assert.Contains(t, err.Error(), "expired")
}

func TestIntegration_UsageLimitEnforcement(t *testing.T) {
	db := setupTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()

	// Create API key with usage limit
	maxUsage := int64(2)
	req := &CreateRequest{
		Name:          "Limited Test Key",
		ExpiresAt:     nil,
		MaxUsageCount: &maxUsage,
	}

	result, err := service.Create(ctx, req)
	require.NoError(t, err)

	// First validation should succeed
	_, err = service.ValidateKey(ctx, result.Token)
	require.NoError(t, err)

	// Fetch updated values from database
	updatedKey, err := service.FindByID(ctx, result.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), updatedKey.UsageCount)

	// Second validation should succeed
	_, err = service.ValidateKey(ctx, result.Token)
	require.NoError(t, err)

	// Fetch updated values from database
	updatedKey, err = service.FindByID(ctx, result.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), updatedKey.UsageCount)

	// Third validation should fail (exceeded limit)
	validated, err := service.ValidateKey(ctx, result.Token)
	assert.Error(t, err)
	assert.Nil(t, validated)
	assert.Contains(t, err.Error(), "usage limit exceeded")
}

func TestIntegration_MiddlewareWithRealDB(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)
	middleware := NewMiddlewareProvider(service)

	ctx := context.Background()

	// Create a real API key
	req := &CreateRequest{
		Name:          "Middleware Test Key",
		ExpiresAt:     nil,
		MaxUsageCount: nil,
	}

	result, err := service.Create(ctx, req)
	require.NoError(t, err)

	// Test middleware with real HTTP request
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/protected", nil)
	c.Request.Header.Set("X-API-Key", result.Token)

	middleware.Auth()(c)

	// Verify middleware succeeded
	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, c.IsAborted())

	// Verify context values
	apiKeyId, exists := c.Get("apiKeyId")
	assert.True(t, exists)
	assert.Equal(t, result.ID, apiKeyId)

	authType, exists := c.Get("authType")
	assert.True(t, exists)
	assert.Equal(t, "api_key", authType)

	// Verify usage count was incremented in database
	updatedKey, err := service.FindByID(ctx, result.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), updatedKey.UsageCount)
	assert.NotNil(t, updatedKey.LastUsed)
}

func TestIntegration_UpdateLastUsed(t *testing.T) {
	db := setupTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()

	// Create API key
	req := &CreateRequest{
		Name:          "Usage Test Key",
		ExpiresAt:     nil,
		MaxUsageCount: nil,
	}

	result, err := service.Create(ctx, req)
	require.NoError(t, err)

	// Verify initial state
	initialKey, err := service.FindByID(ctx, result.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), initialKey.UsageCount)
	assert.Nil(t, initialKey.LastUsed)

	// Validate key (should update usage)
	_, err = service.ValidateKey(ctx, result.Token)
	require.NoError(t, err)

	// Verify usage was updated by fetching from database
	dbKey, err := service.FindByID(ctx, result.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), dbKey.UsageCount)
	assert.NotNil(t, dbKey.LastUsed)
	assert.True(t, dbKey.LastUsed.After(initialKey.CreatedAt))
}

func TestIntegration_CreateWithExpiration(t *testing.T) {
	db := setupTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()

	// Create API key with future expiration
	futureTime := time.Now().Add(1 * time.Hour)
	req := &CreateRequest{
		Name:          "Future Expiry Key",
		ExpiresAt:     &futureTime,
		MaxUsageCount: nil,
	}

	result, err := service.Create(ctx, req)
	require.NoError(t, err)

	// Should be able to validate before expiration
	validated, err := service.ValidateKey(ctx, result.Token)
	require.NoError(t, err)
	assert.Equal(t, result.ID, validated.ID)
	assert.NotNil(t, validated.ExpiresAt)
	assert.Equal(t, futureTime.Unix(), validated.ExpiresAt.Unix())
}

func TestIntegration_CreateWithUsageLimit(t *testing.T) {
	db := setupTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()

	// Create API key with usage limit
	maxUsage := int64(5)
	req := &CreateRequest{
		Name:          "Usage Limited Key",
		ExpiresAt:     nil,
		MaxUsageCount: &maxUsage,
	}

	result, err := service.Create(ctx, req)
	require.NoError(t, err)

	// Verify the key was created with usage limit
	createdKey, err := service.FindByID(ctx, result.ID)
	require.NoError(t, err)
	assert.Equal(t, maxUsage, *createdKey.MaxUsageCount)
	assert.Equal(t, int64(0), createdKey.UsageCount)
}

func TestIntegration_FindAllWithRealData(t *testing.T) {
	db := setupTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := context.Background()

	// Create multiple API keys
	keys := []*CreateRequest{
		{Name: "Key 1", ExpiresAt: nil, MaxUsageCount: nil},
		{Name: "Key 2", ExpiresAt: nil, MaxUsageCount: nil},
		{Name: "Key 3", ExpiresAt: nil, MaxUsageCount: nil},
	}

	var createdKeys []*APIKeyWithToken
	for _, key := range keys {
		result, err := service.Create(ctx, key)
		require.NoError(t, err)
		createdKeys = append(createdKeys, result)
	}

	// Find all keys
	allKeys, err := service.FindAll(ctx)
	require.NoError(t, err)

	// Verify all keys were found
	assert.Len(t, allKeys, 3)

	// Verify keys are ordered by created_at DESC (newest first)
	assert.Equal(t, createdKeys[2].ID, allKeys[0].ID) // Last created should be first
	assert.Equal(t, createdKeys[1].ID, allKeys[1].ID)
	assert.Equal(t, createdKeys[0].ID, allKeys[2].ID) // First created should be last
}
