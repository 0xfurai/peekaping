package api_key

import (
	"net/http"
	"peekaping/src/modules/auth"
	"peekaping/src/utils"
	"strings"

	"github.com/gin-gonic/gin"
)

// HybridMiddlewareProvider provides middleware that supports both JWT and API key authentication
type HybridMiddlewareProvider struct {
	apiKeyService Service
	jwtMiddleware *auth.MiddlewareProvider
}

// NewHybridMiddlewareProvider creates a new hybrid middleware provider
func NewHybridMiddlewareProvider(apiKeyService Service, jwtMiddleware *auth.MiddlewareProvider) *HybridMiddlewareProvider {
	return &HybridMiddlewareProvider{
		apiKeyService: apiKeyService,
		jwtMiddleware: jwtMiddleware,
	}
}

// HybridAuth is a middleware that supports both JWT and API key authentication
func (p *HybridMiddlewareProvider) HybridAuth() gin.HandlerFunc {
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
			apiKey, err := p.apiKeyService.ValidateKey(c, authHeader)
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

		// For JWT tokens, use the existing JWT middleware logic
		// Add Bearer prefix if not present
		if !strings.HasPrefix(authHeader, "Bearer ") {
			authHeader = "Bearer " + authHeader
		}

		// Check if the header has the Bearer prefix
		fields := strings.Fields(authHeader)
		if len(fields) != 2 || fields[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Invalid authorization header format"))
			c.Abort()
			return
		}

		// Extract the token
		accessToken := fields[1]

		// Verify the token using the JWT middleware
		claims, err := p.jwtMiddleware.GetTokenMaker().VerifyToken(c, accessToken, "access")
		if err != nil {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Invalid or expired token"))
			c.Abort()
			return
		}

		// Check if it's an access token
		if claims.Type != "access" {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Invalid token type"))
			c.Abort()
			return
		}

		// Set user information in the context
		c.Set("userId", claims.UserID)
		c.Set("email", claims.Email)
		c.Set("authType", "jwt")

		c.Next()
	}
}
