package auth

import (
	"net/http"
	"peekaping/src/modules/auth/login_attempt"
	"peekaping/src/utils"
	"strings"

	"github.com/gin-gonic/gin"
)

// MiddlewareProvider holds all middleware functions
type MiddlewareProvider struct {
	tokenMaker           *TokenMaker
	bruteforceMiddleware *login_attempt.BruteforceMiddleware
}

// NewMiddlewareProvider creates a new middleware provider
func NewMiddlewareProvider(tokenMaker *TokenMaker, bruteforceMiddleware *login_attempt.BruteforceMiddleware) *MiddlewareProvider {
	return &MiddlewareProvider{
		tokenMaker:           tokenMaker,
		bruteforceMiddleware: bruteforceMiddleware,
	}
}

// Auth is a middleware that verifies the JWT access token
func (p *MiddlewareProvider) Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get the Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Authorization header is required"))
			c.Abort()
			return
		}

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

		// Verify the token
		claims, err := p.tokenMaker.VerifyToken(c, accessToken, "access")
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

		c.Next()
	}
}

// BruteforceProtection returns the bruteforce protection middleware
func (p *MiddlewareProvider) BruteforceProtection() gin.HandlerFunc {
	return p.bruteforceMiddleware.BruteforceProtection()
}

// RecordLoginAttempt records a login attempt result
func (p *MiddlewareProvider) RecordLoginAttempt(c *gin.Context, email string, success bool) {
	p.bruteforceMiddleware.RecordLoginAttempt(c, email, success)
}
