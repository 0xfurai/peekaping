package api_key

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/workspace"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockService implements Service interface for middleware tests
type MockService struct {
	mock.Mock
}

func (m *MockService) Create(ctx context.Context, req *CreateRequest) (*APIKeyWithToken, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*APIKeyWithToken), args.Error(1)
}

func (m *MockService) FindAll(ctx context.Context, workspaceID workspace.ID) ([]*Model, error) {
	args := m.Called(ctx, workspaceID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*Model), args.Error(1)
}

func (m *MockService) FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	args := m.Called(ctx, workspaceID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockService) Update(ctx context.Context, workspaceID workspace.ID, id string, req *UpdateRequest) (*Model, error) {
	args := m.Called(ctx, workspaceID, id, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockService) Delete(ctx context.Context, workspaceID workspace.ID, id string) error {
	args := m.Called(ctx, workspaceID, id)
	return args.Error(0)
}

func (m *MockService) ValidateKey(ctx context.Context, key string) (*Model, error) {
	args := m.Called(ctx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func TestMiddlewareProvider_Auth_MissingHeader(t *testing.T) {

	mockSvc := new(MockService)
	middleware := NewMiddlewareProvider(mockSvc)

	w := httptest.NewRecorder()
	ran := false
	req := httptest.NewRequest("GET", "/", nil)
	// No X-API-Key header set

	middleware.Auth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ran = true })).ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.False(t, ran)

	// Check response body directly
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Contains(t, resp["message"], "X-API-Key header is required")
	assert.Nil(t, resp["data"])

	mockSvc.AssertExpectations(t)
}

func TestMiddlewareProvider_Auth_InvalidPrefix(t *testing.T) {

	mockSvc := new(MockService)
	middleware := NewMiddlewareProvider(mockSvc)

	w := httptest.NewRecorder()
	ran := false
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", "invalid-prefix-token")

	middleware.Auth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ran = true })).ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.False(t, ran)

	// Check response body directly
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Contains(t, resp["message"], "API key required")
	assert.Nil(t, resp["data"])

	mockSvc.AssertExpectations(t)
}

func TestMiddlewareProvider_Auth_ValidKey(t *testing.T) {

	mockSvc := new(MockService)
	middleware := NewMiddlewareProvider(mockSvc)

	validToken := ApiKeyPrefix + "eyJpZCI6InRlc3Qta2V5LWlkIiwia2V5IjoidGVzdC1hY3R1YWwta2V5In0="
	apiKey := &Model{
		ID:         "test-key-id",
		Name:       "Test Key",
		DisplayKey: "pk_test...",
	}

	mockSvc.On("ValidateKey", mock.Anything, validToken).Return(apiKey, nil)

	w := httptest.NewRecorder()
	var seen *http.Request
	ran := false
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", validToken)

	middleware.Auth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ran = true; seen = r })).ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, ran)

	// Verify context values are set
	apiKeyId, exists := httpx.Lookup(seen.Context(), "apiKeyId")
	assert.True(t, exists)
	assert.Equal(t, "test-key-id", apiKeyId)

	authType, exists := httpx.Lookup(seen.Context(), "authType")
	assert.True(t, exists)
	assert.Equal(t, "api_key", authType)

	mockSvc.AssertExpectations(t)
}

func TestMiddlewareProvider_Auth_ExpiredKey(t *testing.T) {

	mockSvc := new(MockService)
	middleware := NewMiddlewareProvider(mockSvc)

	expiredToken := ApiKeyPrefix + "eyJpZCI6ImV4cGlyZWQta2V5LWlkIiwia2V5IjoidGVzdC1hY3R1YWwta2V5In0="

	mockSvc.On("ValidateKey", mock.Anything, expiredToken).Return(nil, errors.New("API key has expired"))

	w := httptest.NewRecorder()
	ran := false
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", expiredToken)

	middleware.Auth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ran = true })).ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.False(t, ran)

	// Check response body directly
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Contains(t, resp["message"], "Invalid or expired API key")
	assert.Nil(t, resp["data"])

	mockSvc.AssertExpectations(t)
}

func TestMiddlewareProvider_Auth_InvalidKey(t *testing.T) {

	mockSvc := new(MockService)
	middleware := NewMiddlewareProvider(mockSvc)

	invalidToken := ApiKeyPrefix + "eyJpZCI6ImludmFsaWQta2V5LWlkIiwia2V5IjoidGVzdC1hY3R1YWwta2V5In0="

	mockSvc.On("ValidateKey", mock.Anything, invalidToken).Return(nil, errors.New("Invalid API key"))

	w := httptest.NewRecorder()
	ran := false
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", invalidToken)

	middleware.Auth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ran = true })).ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.False(t, ran)

	// Check response body directly
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Contains(t, resp["message"], "Invalid or expired API key")
	assert.Nil(t, resp["data"])

	mockSvc.AssertExpectations(t)
}

func TestMiddlewareProvider_Auth_ContextValues(t *testing.T) {

	mockSvc := new(MockService)
	middleware := NewMiddlewareProvider(mockSvc)

	validToken := ApiKeyPrefix + "eyJpZCI6InRlc3Qta2V5LWlkIiwia2V5IjoidGVzdC1hY3R1YWwta2V5In0="
	apiKey := &Model{
		ID:         "context-test-key",
		Name:       "Context Test Key",
		DisplayKey: "pk_context...",
	}

	mockSvc.On("ValidateKey", mock.Anything, validToken).Return(apiKey, nil)

	w := httptest.NewRecorder()
	var seen *http.Request
	ran := false
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", validToken)

	middleware.Auth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ran = true; seen = r })).ServeHTTP(w, req)

	assert.True(t, ran)
	// Verify specific context values
	apiKeyId, exists := httpx.Lookup(seen.Context(), "apiKeyId")
	assert.True(t, exists)
	assert.Equal(t, "context-test-key", apiKeyId)

	authType, exists := httpx.Lookup(seen.Context(), "authType")
	assert.True(t, exists)
	assert.Equal(t, "api_key", authType)

	// Verify no other auth-related context values are set
	_, jwtExists := httpx.Lookup(seen.Context(), "userId")
	assert.False(t, jwtExists)

	mockSvc.AssertExpectations(t)
}

func TestMiddlewareProvider_Auth_ServiceError(t *testing.T) {

	mockSvc := new(MockService)
	middleware := NewMiddlewareProvider(mockSvc)

	validToken := ApiKeyPrefix + "eyJpZCI6InNlcnZpY2UtZXJyb3Ita2V5Iiwia2V5IjoidGVzdC1hY3R1YWwta2V5In0="

	mockSvc.On("ValidateKey", mock.Anything, validToken).Return(nil, errors.New("service error"))

	w := httptest.NewRecorder()
	ran := false
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", validToken)

	middleware.Auth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ran = true })).ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.False(t, ran)

	// Check response body directly
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Contains(t, resp["message"], "Invalid or expired API key")
	assert.Nil(t, resp["data"])

	mockSvc.AssertExpectations(t)
}
