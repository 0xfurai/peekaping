package api_key

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
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

	ctx := adminContext()

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
	updatedKey, err := service.FindByID(ctx, testWorkspace, result.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), updatedKey.UsageCount)
	assert.NotNil(t, updatedKey.LastUsed)
}

func TestServiceImpl_ValidateKey_ExpiredKey(t *testing.T) {
	db := setupServiceTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := adminContext()

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

	ctx := adminContext()

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

	ctx := adminContext()
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

	ctx := adminContext()

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
	payload, err := json.Marshal(map[string]string{
		"id":  result.ID,
		"key": base64.URLEncoding.EncodeToString(make([]byte, ApiKeyRandomBytes)),
	})
	require.NoError(t, err)
	fakeToken := ApiKeyPrefix + base64.URLEncoding.EncodeToString(payload)

	// Try to validate with wrong key
	validated, err := service.ValidateKey(ctx, fakeToken)
	assert.Error(t, err)
	assert.Nil(t, validated)
	assert.Contains(t, err.Error(), "invalid API key")

	// The same ID with the right key validates, so the key is what failed
	validated, err = service.ValidateKey(ctx, result.Token)
	require.NoError(t, err)
	assert.Equal(t, result.ID, validated.ID)
}

func TestServiceImpl_ValidateKey_NotFound(t *testing.T) {
	db := setupServiceTestDB(t)
	repo := NewSQLRepository(db)
	logger := zap.NewNop().Sugar()
	service := NewService(repo, logger)

	ctx := adminContext()
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

	ctx := adminContext()
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

	ctx := adminContext()

	// Create multiple API keys
	req1 := &CreateRequest{Name: "Key 1"}
	req2 := &CreateRequest{Name: "Key 2"}

	_, err := service.Create(ctx, req1)
	require.NoError(t, err)
	_, err = service.Create(ctx, req2)
	require.NoError(t, err)

	result, err := service.FindAll(ctx, testWorkspace)
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

	ctx := adminContext()

	// Create an API key
	req := &CreateRequest{Name: "Test Key"}
	created, err := service.Create(ctx, req)
	require.NoError(t, err)

	// Find it by ID
	result, err := service.FindByID(ctx, testWorkspace, created.ID)
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

	ctx := adminContext()

	// Create an API key
	req := &CreateRequest{Name: "Original Name"}
	created, err := service.Create(ctx, req)
	require.NoError(t, err)

	// Update it
	updateReq := &UpdateRequest{
		Name: stringPtr("Updated Name"),
	}

	result, err := service.Update(ctx, testWorkspace, created.ID, updateReq)
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

	ctx := adminContext()

	// Create an API key
	req := &CreateRequest{Name: "Test Key"}
	created, err := service.Create(ctx, req)
	require.NoError(t, err)

	// Verify it exists
	_, err = service.FindByID(ctx, testWorkspace, created.ID)
	assert.NoError(t, err)

	// Delete it
	err = service.Delete(ctx, testWorkspace, created.ID)
	assert.NoError(t, err)

	// Verify it's gone
	deletedKey, err := service.FindByID(ctx, testWorkspace, created.ID)
	assert.NoError(t, err)
	assert.Nil(t, deletedKey)
}

// A MongoDB install imported into SQL keeps its API key tokens, which carry the
// key's ObjectID, while the key's row has the UUID that ObjectID maps to.
func TestServiceImpl_ValidateKey_TokenWithImportedObjectID(t *testing.T) {
	db := setupServiceTestDB(t)
	service := NewService(NewSQLRepository(db), zap.NewNop().Sugar())
	ctx := adminContext()

	created, err := service.Create(ctx, &CreateRequest{Name: "Imported key"})
	require.NoError(t, err)
	// The key as the import writes it: the UUID computed outside Go for this ObjectID
	const objectID, importedID = "507f1f77bcf86cd799439011", "2ec0f307-6408-57cd-b5a8-08c55d4ab445"
	_, err = db.Exec("UPDATE api_keys SET id = ? WHERE id = ?", importedID, created.ID)
	require.NoError(t, err)

	tokenFor := func(id string) string {
		payload := decodeToken(t, created.Token)
		payload["id"] = id
		encoded, err := json.Marshal(payload)
		require.NoError(t, err)
		return ApiKeyPrefix + base64.URLEncoding.EncodeToString(encoded)
	}

	validated, err := service.ValidateKey(ctx, tokenFor(objectID))
	require.NoError(t, err)
	assert.Equal(t, importedID, validated.ID)
	assert.Equal(t, "Imported key", validated.Name)

	// The mapped ID in the token works too, and other IDs still find nothing
	validated, err = service.ValidateKey(ctx, tokenFor(importedID))
	require.NoError(t, err)
	assert.Equal(t, importedID, validated.ID)
	_, err = service.ValidateKey(ctx, tokenFor("ffffffffffffffffffffffff"))
	assert.EqualError(t, err, "invalid API key")

	// The secret still has to match
	wrong := decodeToken(t, created.Token)
	wrong["id"] = objectID
	wrong["key"] = base64.URLEncoding.EncodeToString(make([]byte, ApiKeyRandomBytes))
	encoded, err := json.Marshal(wrong)
	require.NoError(t, err)
	_, err = service.ValidateKey(ctx, ApiKeyPrefix+base64.URLEncoding.EncodeToString(encoded))
	assert.EqualError(t, err, "invalid API key")
}

func decodeToken(t *testing.T, token string) map[string]string {
	t.Helper()
	payload, err := base64.URLEncoding.DecodeString(token[len(ApiKeyPrefix):])
	require.NoError(t, err)
	var fields map[string]string
	require.NoError(t, json.Unmarshal(payload, &fields))
	return fields
}

// tokenWith is the token that names the key with this ID and carries secret.
func tokenWith(t *testing.T, id, secret string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"id": id, "key": secret})
	require.NoError(t, err)
	return ApiKeyPrefix + base64.URLEncoding.EncodeToString(payload)
}

// storeKey adds a key with the given stored hash, as an earlier release or this one
// would have, and returns its ID.
func storeKey(t *testing.T, repo Repository, keyHash string) string {
	t.Helper()
	created, err := repo.Create(adminContext(), &CreateModel{WorkspaceID: testWorkspace, Name: "Stored key", KeyHash: keyHash, DisplayKey: "pk_stored..."})
	require.NoError(t, err)
	return created.ID
}

func TestServiceImpl_Create_StoresSHA256Hash(t *testing.T) {
	db := setupServiceTestDB(t)
	service := NewService(NewSQLRepository(db), zap.NewNop().Sugar())
	ctx := adminContext()

	created, err := service.Create(ctx, &CreateRequest{Name: "Hashed key"})
	require.NoError(t, err)

	stored, err := service.FindByID(ctx, testWorkspace, created.ID)
	require.NoError(t, err)
	assert.Regexp(t, `^sha256\$[0-9a-f]{64}$`, stored.KeyHash)
	_, err = service.ValidateKey(ctx, created.Token)
	assert.NoError(t, err)
}

func TestServiceImpl_ValidateKey_ComparesSHA256Hash(t *testing.T) {
	db := setupServiceTestDB(t)
	repo := NewSQLRepository(db)
	service := NewService(repo, zap.NewNop().Sugar())
	ctx := adminContext()

	// sha256("secret"), worked out outside the code
	id := storeKey(t, repo, "sha256$2bb80d537b1da3e38bd30361aa855686bde0eacd7162fef6a25fe97bf527a25b")

	validated, err := service.ValidateKey(ctx, tokenWith(t, id, "secret"))
	require.NoError(t, err)
	assert.Equal(t, id, validated.ID)

	_, err = service.ValidateKey(ctx, tokenWith(t, id, "other-secret"))
	assert.EqualError(t, err, "invalid API key")
	_, err = service.ValidateKey(ctx, tokenWith(t, id, ""))
	assert.EqualError(t, err, "invalid API key")

	// A key stored with no usable hash accepts nothing
	for _, keyHash := range []string{"", "secret", "sha256$", "md5$5ebe2294ecd0e0f08eab7690d2a6ee69"} {
		_, err = service.ValidateKey(ctx, tokenWith(t, storeKey(t, repo, keyHash), "secret"))
		assert.EqualError(t, err, "invalid API key", "stored hash %q", keyHash)
	}
}

// A key created before the SHA-256 hash was used is stored as a bcrypt hash. It
// still works, and its hash is replaced by a SHA-256 hash on its first successful use.
func TestServiceImpl_ValidateKey_ReplacesBcryptHashOnFirstUse(t *testing.T) {
	db := setupServiceTestDB(t)
	repo := NewSQLRepository(db)
	service := NewService(repo, zap.NewNop().Sugar())
	ctx := adminContext()
	legacy, err := bcrypt.GenerateFromPassword([]byte("legacy-secret"), bcrypt.MinCost)
	require.NoError(t, err)
	id := storeKey(t, repo, string(legacy))

	_, err = service.ValidateKey(ctx, tokenWith(t, id, "other-secret"))
	assert.EqualError(t, err, "invalid API key")
	stored, err := service.FindByID(ctx, testWorkspace, id)
	require.NoError(t, err)
	assert.Equal(t, string(legacy), stored.KeyHash, "a wrong key leaves the hash alone")

	validated, err := service.ValidateKey(ctx, tokenWith(t, id, "legacy-secret"))
	require.NoError(t, err)
	assert.Equal(t, id, validated.ID)

	// sha256("legacy-secret"), worked out outside the code
	stored, err = service.FindByID(ctx, testWorkspace, id)
	require.NoError(t, err)
	assert.Equal(t, "sha256$fdcbc807d80f60c6f15ef644d5c372ac92760bd5f414cc3d48c3b320d9d1e689", stored.KeyHash)
	assert.Equal(t, "pk_stored...", stored.DisplayKey)
	assert.Equal(t, "Stored key", stored.Name)

	_, err = service.ValidateKey(ctx, tokenWith(t, id, "legacy-secret"))
	assert.NoError(t, err, "the key works after its hash was replaced")
	_, err = service.ValidateKey(ctx, tokenWith(t, id, "other-secret"))
	assert.EqualError(t, err, "invalid API key")
}

// Helper function for string pointers
func stringPtr(s string) *string {
	return &s
}
