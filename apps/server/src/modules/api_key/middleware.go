package api_key

import (
	"net/http"
	"peekaping/src/utils"
	"strings"

	"github.com/gin-gonic/gin"
)

// MiddlewareProvider holds API key middleware functions
type MiddlewareProvider struct {
	service Service
}

// NewMiddlewareProvider creates a new API key middleware provider
func NewMiddlewareProvider(service Service) *MiddlewareProvider {
	return &MiddlewareProvider{
		service: service,
	}
}

// Auth is a middleware that verifies API key authentication
// This should be used as the final middleware in a chain for API key-only endpoints
func (p *MiddlewareProvider) Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
	// Get the X-API-Key header
	authHeader := c.GetHeader("X-API-Key")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, utils.NewFailResponse("X-API-Key header is required"))
		c.Abort()
		return
	}

		// Only accept API keys
		if !strings.HasPrefix(authHeader, ApiKeyPrefix) {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("API key required"))
			c.Abort()
			return
		}

		// Validate the API key
		apiKey, err := p.service.ValidateKey(c, authHeader)
		if err != nil {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Invalid or expired API key"))
			c.Abort()
			return
		}

		// Set API key information in the context
		c.Set("apiKeyId", apiKey.ID)
		c.Set("authType", "api_key")

		c.Next()
	}
}

