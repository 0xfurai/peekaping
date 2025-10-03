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

// APIKeyAuth is a middleware that verifies API key authentication
func (p *MiddlewareProvider) APIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get the Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Authorization header is required"))
			c.Abort()
			return
		}

		// Check if it's an API key (starts with pk_)
		if strings.HasPrefix(authHeader, "pk_") {
			// Validate the API key
			apiKey, err := p.service.ValidateKey(c, authHeader)
			if err != nil {
				c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Invalid or expired API key"))
				c.Abort()
				return
			}

			// Set user information in the context
			c.Set("userId", apiKey.UserID)
			c.Set("email", "") // API keys don't have email directly
			c.Set("apiKeyId", apiKey.ID)
			c.Set("authType", "api_key")

			c.Next()
			return
		}

		// If it's not an API key, let the request continue to JWT middleware
		// This allows both JWT and API key authentication on the same endpoints
		c.Next()
	}
}

// APIKeyOnlyAuth is a middleware that only accepts API key authentication
func (p *MiddlewareProvider) APIKeyOnlyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get the Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Authorization header is required"))
			c.Abort()
			return
		}

		// Only accept API keys
		if !strings.HasPrefix(authHeader, "pk_") {
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

		// Set user information in the context
		c.Set("userId", apiKey.UserID)
		c.Set("email", "") // API keys don't have email directly
		c.Set("apiKeyId", apiKey.ID)
		c.Set("authType", "api_key")

		c.Next()
	}
}
