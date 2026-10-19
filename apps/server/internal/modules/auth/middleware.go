package auth

import (
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/utils"
	"strings"
)

// MiddlewareProvider holds all middleware functions
type MiddlewareProvider struct {
	tokenMaker *TokenMaker
}

// NewMiddlewareProvider creates a new middleware provider
func NewMiddlewareProvider(tokenMaker *TokenMaker) *MiddlewareProvider {
	return &MiddlewareProvider{
		tokenMaker: tokenMaker,
	}
}

// Auth is a middleware that verifies JWT access tokens only
func (p *MiddlewareProvider) Auth() httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get the Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				httpx.JSON(w, http.StatusUnauthorized, utils.NewFailResponse("Authorization header is required"))

				return
			}

			// JWT authentication
			// Add Bearer prefix if not present
			if !strings.HasPrefix(authHeader, "Bearer ") {
				authHeader = "Bearer " + authHeader
			}

			// Check if the header has the Bearer prefix
			fields := strings.Fields(authHeader)
			if len(fields) != 2 || fields[0] != "Bearer" {
				httpx.JSON(w, http.StatusUnauthorized, utils.NewFailResponse("Invalid authorization header format"))

				return
			}

			// Extract the token
			accessToken := fields[1]

			// Verify the token
			claims, err := p.tokenMaker.VerifyToken(r.Context(), accessToken, "access")
			if err != nil {
				httpx.JSON(w, http.StatusUnauthorized, utils.NewFailResponse("Invalid or expired token"))

				return
			}

			// Check if it's an access token
			if claims.Type != "access" {
				httpx.JSON(w, http.StatusUnauthorized, utils.NewFailResponse("Invalid token type"))

				return
			}

			// Set user information in the context
			r = httpx.WithValue(r, "userId", claims.UserID)
			r = httpx.WithValue(r, "email", claims.Email)
			r = httpx.WithValue(r, "authType", "jwt")

			next.ServeHTTP(w, r)
		})
	}
}
