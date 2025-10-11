package middleware

import (
	"net/http"
	"peekaping/src/modules/api_key"
	"peekaping/src/modules/auth"
	"peekaping/src/utils"
	"strings"

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

// AllAuth creates a middleware that tries JWT first, then API key
func (ac *AuthChain) AllAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			ac.logger.Warnw("Missing Authorization header", "ip", c.ClientIP(), "path", c.Request.URL.Path)
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Authorization header is required"))
			c.Abort()
			return
		}

		isApiKey := strings.HasPrefix(authHeader, api_key.ApiKeyPrefix)
		if isApiKey {
			ac.logger.Infow("Routing to API key authentication", "ip", c.ClientIP(), "path", c.Request.URL.Path, "keyPrefix", authHeader[:min(len(authHeader), 10)]+"...")
			ac.apiKeyMiddleware.Auth()(c)
		} else {
			ac.logger.Infow("Routing to JWT authentication", "ip", c.ClientIP(), "path", c.Request.URL.Path, "tokenPrefix", authHeader[:min(len(authHeader), 10)]+"...")
			ac.jwtMiddleware.Auth()(c)
		}
	}
}
