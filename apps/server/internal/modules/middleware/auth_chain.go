package middleware

import (
	"net/http"
	"vigi/internal/modules/api_key"
	"vigi/internal/modules/auth"
	"vigi/internal/utils"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// MARK: Types and Constructor

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

// MARK: AllAuth

// AllAuth creates a middleware that supports both JWT and API key authentication
// The middleware automatically routes requests based on header presence:
// - If X-API-Key header is present: routes to API key authentication
// - Otherwise: routes to JWT authentication (expects Authorization header with Bearer token)
func (ac *AuthChain) AllAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKeyHeader := c.GetHeader("X-API-Key")
		authHeader := c.GetHeader("Authorization")

		if apiKeyHeader != "" {
			// Route to API key authentication
			ac.logger.Debugw("Routing to API key authentication", "ip", c.ClientIP(), "path", c.Request.URL.Path, "keyPrefix", apiKeyHeader[:min(len(apiKeyHeader), 10)]+"...")
			ac.apiKeyMiddleware.Auth()(c)
		} else if authHeader != "" {
			// Route to JWT authentication
			ac.logger.Debugw("Routing to JWT authentication", "ip", c.ClientIP(), "path", c.Request.URL.Path, "tokenPrefix", authHeader[:min(len(authHeader), 10)]+"...")
			ac.jwtMiddleware.Auth()(c)
		} else {
			// No authentication headers provided
			ac.logger.Warnw("Missing authentication headers", "ip", c.ClientIP(), "path", c.Request.URL.Path)
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Authentication required: provide either X-API-Key header or Authorization header"))
			c.Abort()
			return
		}
	}
}
