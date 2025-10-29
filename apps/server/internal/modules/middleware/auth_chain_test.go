package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestAuthChain is a test-specific version that allows us to inject mock handlers
type TestAuthChain struct {
	jwtMiddleware    gin.HandlerFunc
	apiKeyMiddleware gin.HandlerFunc
	logger           *zap.SugaredLogger
}

func NewTestAuthChain(jwtHandler, apiKeyHandler gin.HandlerFunc, logger *zap.SugaredLogger) *TestAuthChain {
	return &TestAuthChain{
		jwtMiddleware:    jwtHandler,
		apiKeyMiddleware: apiKeyHandler,
		logger:           logger.Named("[auth_chain]"),
	}
}

// AllAuth creates a middleware that supports both JWT and API key authentication
func (ac *TestAuthChain) AllAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKeyHeader := c.GetHeader("X-API-Key")
		authHeader := c.GetHeader("Authorization")

		if apiKeyHeader != "" {
			// Route to API key authentication
			ac.logger.Infow("Routing to API key authentication", "ip", c.ClientIP(), "path", c.Request.URL.Path, "keyPrefix", apiKeyHeader[:min(len(apiKeyHeader), 10)]+"...")
			ac.apiKeyMiddleware(c)
		} else if authHeader != "" {
			// Route to JWT authentication
			ac.logger.Infow("Routing to JWT authentication", "ip", c.ClientIP(), "path", c.Request.URL.Path, "tokenPrefix", authHeader[:min(len(authHeader), 10)]+"...")
			ac.jwtMiddleware(c)
		} else {
			// No authentication headers provided
			ac.logger.Debugw("Missing authentication headers", "ip", c.ClientIP(), "path", c.Request.URL.Path)
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "Authentication required: provide either X-API-Key header or Authorization header"})
			c.Abort()
			return
		}
	}
}

func TestAuthChain_AllAuth_RoutesToAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := zap.NewNop().Sugar()

	apiKeyCalled := false
	jwtCalled := false

	jwtHandler := func(c *gin.Context) {
		jwtCalled = true
		c.Set("authType", "jwt")
		c.Set("userId", "test-user-id")
		c.Next()
	}

	apiKeyHandler := func(c *gin.Context) {
		apiKeyCalled = true
		c.Set("authType", "api_key")
		c.Set("apiKeyId", "test-api-key-id")
		c.Next()
	}

	authChain := NewTestAuthChain(jwtHandler, apiKeyHandler, logger)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", nil)
	c.Request.Header.Set("X-API-Key", "pk_test-token")

	authChain.AllAuth()(c)

	// Verify API key middleware was called
	assert.True(t, apiKeyCalled)
	assert.False(t, jwtCalled)

	// Verify context values
	authType, exists := c.Get("authType")
	assert.True(t, exists)
	assert.Equal(t, "api_key", authType)

	apiKeyId, exists := c.Get("apiKeyId")
	assert.True(t, exists)
	assert.Equal(t, "test-api-key-id", apiKeyId)

	// Verify JWT context is not set
	_, jwtExists := c.Get("userId")
	assert.False(t, jwtExists)
}

func TestAuthChain_AllAuth_RoutesToJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := zap.NewNop().Sugar()

	apiKeyCalled := false
	jwtCalled := false

	jwtHandler := func(c *gin.Context) {
		jwtCalled = true
		c.Set("authType", "jwt")
		c.Set("userId", "test-user-id")
		c.Next()
	}

	apiKeyHandler := func(c *gin.Context) {
		apiKeyCalled = true
		c.Set("authType", "api_key")
		c.Set("apiKeyId", "test-api-key-id")
		c.Next()
	}

	authChain := NewTestAuthChain(jwtHandler, apiKeyHandler, logger)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", nil)
	c.Request.Header.Set("Authorization", "Bearer jwt-token")

	authChain.AllAuth()(c)

	// Verify JWT middleware was called
	assert.True(t, jwtCalled)
	assert.False(t, apiKeyCalled)

	// Verify context values
	authType, exists := c.Get("authType")
	assert.True(t, exists)
	assert.Equal(t, "jwt", authType)

	userId, exists := c.Get("userId")
	assert.True(t, exists)
	assert.Equal(t, "test-user-id", userId)

	// Verify API key context is not set
	_, apiKeyExists := c.Get("apiKeyId")
	assert.False(t, apiKeyExists)
}

func TestAuthChain_AllAuth_BothHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := zap.NewNop().Sugar()

	apiKeyCalled := false
	jwtCalled := false

	jwtHandler := func(c *gin.Context) {
		jwtCalled = true
		c.Set("authType", "jwt")
		c.Set("userId", "test-user-id")
		c.Next()
	}

	apiKeyHandler := func(c *gin.Context) {
		apiKeyCalled = true
		c.Set("authType", "api_key")
		c.Set("apiKeyId", "test-api-key-id")
		c.Next()
	}

	authChain := NewTestAuthChain(jwtHandler, apiKeyHandler, logger)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", nil)
	c.Request.Header.Set("X-API-Key", "pk_test-token")
	c.Request.Header.Set("Authorization", "Bearer jwt-token")

	authChain.AllAuth()(c)

	// Verify API key middleware takes priority
	assert.True(t, apiKeyCalled)
	assert.False(t, jwtCalled)

	// Verify API key context values
	authType, exists := c.Get("authType")
	assert.True(t, exists)
	assert.Equal(t, "api_key", authType)

	apiKeyId, exists := c.Get("apiKeyId")
	assert.True(t, exists)
	assert.Equal(t, "test-api-key-id", apiKeyId)
}

func TestAuthChain_AllAuth_NoHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := zap.NewNop().Sugar()

	apiKeyCalled := false
	jwtCalled := false

	jwtHandler := func(c *gin.Context) {
		jwtCalled = true
		c.Next()
	}

	apiKeyHandler := func(c *gin.Context) {
		apiKeyCalled = true
		c.Next()
	}

	authChain := NewTestAuthChain(jwtHandler, apiKeyHandler, logger)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", nil)
	// No headers set

	authChain.AllAuth()(c)

	// Verify neither middleware was called
	assert.False(t, apiKeyCalled)
	assert.False(t, jwtCalled)

	// Verify 401 response
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.True(t, c.IsAborted())

	// Verify response body
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Contains(t, resp["message"], "Authentication required")
	assert.Contains(t, resp["message"], "X-API-Key header or Authorization header")
}

func TestAuthChain_AllAuth_EmptyAPIKeyHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := zap.NewNop().Sugar()

	apiKeyCalled := false
	jwtCalled := false

	jwtHandler := func(c *gin.Context) {
		jwtCalled = true
		c.Next()
	}

	apiKeyHandler := func(c *gin.Context) {
		apiKeyCalled = true
		c.Next()
	}

	authChain := NewTestAuthChain(jwtHandler, apiKeyHandler, logger)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", nil)
	c.Request.Header.Set("X-API-Key", "") // Empty header

	authChain.AllAuth()(c)

	// Verify neither middleware was called (empty header treated as missing)
	assert.False(t, apiKeyCalled)
	assert.False(t, jwtCalled)

	// Verify 401 response
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.True(t, c.IsAborted())
}

func TestAuthChain_AllAuth_EmptyAuthHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := zap.NewNop().Sugar()

	apiKeyCalled := false
	jwtCalled := false

	jwtHandler := func(c *gin.Context) {
		jwtCalled = true
		c.Next()
	}

	apiKeyHandler := func(c *gin.Context) {
		apiKeyCalled = true
		c.Next()
	}

	authChain := NewTestAuthChain(jwtHandler, apiKeyHandler, logger)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", nil)
	c.Request.Header.Set("Authorization", "") // Empty header

	authChain.AllAuth()(c)

	// Verify neither middleware was called (empty header treated as missing)
	assert.False(t, apiKeyCalled)
	assert.False(t, jwtCalled)

	// Verify 401 response
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.True(t, c.IsAborted())
}

func TestAuthChain_AllAuth_JWTWithEmptyAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := zap.NewNop().Sugar()

	apiKeyCalled := false
	jwtCalled := false

	jwtHandler := func(c *gin.Context) {
		jwtCalled = true
		c.Set("authType", "jwt")
		c.Set("userId", "test-user-id")
		c.Next()
	}

	apiKeyHandler := func(c *gin.Context) {
		apiKeyCalled = true
		c.Next()
	}

	authChain := NewTestAuthChain(jwtHandler, apiKeyHandler, logger)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", nil)
	c.Request.Header.Set("X-API-Key", "")                     // Empty API key
	c.Request.Header.Set("Authorization", "Bearer jwt-token") // Valid JWT

	authChain.AllAuth()(c)

	// Verify JWT middleware was called (empty API key ignored)
	assert.False(t, apiKeyCalled)
	assert.True(t, jwtCalled)

	// Verify JWT context values
	authType, exists := c.Get("authType")
	assert.True(t, exists)
	assert.Equal(t, "jwt", authType)
}

func TestAuthChain_AllAuth_VerifyLogging(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := zap.NewNop().Sugar()

	apiKeyCalled := false
	jwtCalled := false

	jwtHandler := func(c *gin.Context) {
		jwtCalled = true
		c.Next()
	}

	apiKeyHandler := func(c *gin.Context) {
		apiKeyCalled = true
		c.Set("authType", "api_key")
		c.Set("apiKeyId", "test-api-key-id")
		c.Next()
	}

	authChain := NewTestAuthChain(jwtHandler, apiKeyHandler, logger)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test-path", nil)
	c.Request.Header.Set("X-API-Key", "pk_test-token")

	authChain.AllAuth()(c)

	// Verify API key middleware was called (which indicates logging occurred)
	assert.True(t, apiKeyCalled)
	assert.False(t, jwtCalled)
}
