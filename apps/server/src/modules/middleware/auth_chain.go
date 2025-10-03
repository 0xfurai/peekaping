package middleware

import (
	"peekaping/src/modules/api_key"
	"peekaping/src/modules/auth"
	"peekaping/src/utils"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AuthChain provides a chained authentication middleware
type AuthChain struct {
	jwtMiddleware    *auth.MiddlewareProvider
	apiKeyMiddleware *api_key.MiddlewareProvider
	logger           *zap.SugaredLogger
}

// NewAuthChain creates a new authentication chain
func NewAuthChain(
	jwtMiddleware *auth.MiddlewareProvider,
	apiKeyMiddleware *api_key.MiddlewareProvider,
	logger *zap.SugaredLogger,
) *AuthChain {
	return &AuthChain{
		jwtMiddleware:    jwtMiddleware,
		apiKeyMiddleware: apiKeyMiddleware,
		logger:           logger.Named("[auth_chain]"),
	}
}

// JWTAuth creates a middleware that only accepts JWT authentication
func (ac *AuthChain) JWTOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(401, utils.NewFailResponse("Authorization header is required"))
			c.Abort()
			return
		}

		// Reject API keys explicitly
		if strings.HasPrefix(authHeader, "pk_") {
			c.JSON(401, utils.NewFailResponse("JWT token required. This endpoint does not accept API keys."))
			c.Abort()
			return
		}

		// Use JWT middleware
		ac.jwtMiddleware.Auth()(c)
	}
}

// APIKeyAuth creates a middleware that only accepts API key authentication
func (ac *AuthChain) APIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Use the API key middleware directly
		ac.apiKeyMiddleware.Auth()(c)
	}
}

// AllAuth creates a middleware that tries JWT first, then API key
func (ac *AuthChain) AllAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(401, utils.NewFailResponse("Authorization header is required"))
			c.Abort()
			return
		}

		// Try JWT authentication first
		if ac.tryJWTAuth(c, authHeader) {
			return
		}

		// Try API key authentication
		if ac.tryAPIKeyAuth(c, authHeader) {
			return
		}

		// Both failed, abort
		ac.logger.Warnw("Authentication failed for both JWT and API key", "ip", c.ClientIP())
		c.JSON(401, utils.NewFailResponse("Invalid authentication credentials"))
		c.Abort()
	}
}

// tryJWTAuth attempts JWT authentication
func (ac *AuthChain) tryJWTAuth(c *gin.Context, authHeader string) bool {
	// Skip if it's clearly an API key
	if strings.HasPrefix(authHeader, "pk_") {
		return false
	}

	// Store the original abort state
	wasAborted := c.IsAborted()
	
	// Use the JWT middleware on original context
	ac.jwtMiddleware.Auth()(c)
	
	// Check if authentication was successful
	if c.IsAborted() && !wasAborted {
		return false
	}

	ac.logger.Infow("JWT authentication successful", "ip", c.ClientIP())
	return true
}

// tryAPIKeyAuth attempts API key authentication
func (ac *AuthChain) tryAPIKeyAuth(c *gin.Context, authHeader string) bool {
	// Only try API key if it starts with pk_
	if !strings.HasPrefix(authHeader, "pk_") {
		return false
	}

	// Store the original abort state
	wasAborted := c.IsAborted()
	
	// Use the API key middleware on original context
	ac.apiKeyMiddleware.Auth()(c)
	
	// Check if authentication was successful
	if c.IsAborted() && !wasAborted {
		return false
	}

	ac.logger.Infow("API key authentication successful", "ip", c.ClientIP())
	return true
}
