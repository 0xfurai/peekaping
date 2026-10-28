package middleware

import (
	"context"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/utils"

	"go.uber.org/zap"
)

// MARK: Types and Constructor

// authenticator is a middleware that checks one kind of credential,
// such as the JWT or API key middleware
type authenticator interface {
	Auth() httpx.Middleware
}

// memberships lists the workspaces a user is in. workspace.Repository is one.
type memberships interface {
	ListMemberships(ctx context.Context, userID string) ([]workspace.Membership, error)
}

// users finds a user by ID. auth.Repository is one.
type users interface {
	FindByID(ctx context.Context, id string) (*auth.Model, error)
}

// AuthChain provides a chained authentication middleware
type AuthChain struct {
	jwtMiddleware    authenticator
	apiKeyMiddleware authenticator
	workspaces       memberships
	users            users
	trustedProxies   *httpx.TrustedProxies
	logger           *zap.SugaredLogger
}

// NewAuthChain creates a new authentication chain
func NewAuthChain(
	jwtMiddleware authenticator,
	apiKeyMiddleware authenticator,
	workspaces memberships,
	users users,
	trustedProxies *httpx.TrustedProxies,
	logger *zap.SugaredLogger,
) *AuthChain {
	return &AuthChain{
		jwtMiddleware:    jwtMiddleware,
		apiKeyMiddleware: apiKeyMiddleware,
		workspaces:       workspaces,
		users:            users,
		trustedProxies:   trustedProxies,
		logger:           logger.Named("[auth_chain]"),
	}
}

// MARK: AllAuth

// AllAuth creates a middleware that supports both JWT and API key authentication
// The middleware automatically routes requests based on header presence:
// - If X-API-Key header is present: routes to API key authentication
// - Otherwise: routes to JWT authentication (expects Authorization header with Bearer token)
func (ac *AuthChain) AllAuth() httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKeyHeader := r.Header.Get("X-API-Key")
			authHeader := r.Header.Get("Authorization")

			if apiKeyHeader != "" {
				// Route to API key authentication
				ac.logger.Debugw("Routing to API key authentication", "ip", ac.trustedProxies.ClientIP(r), "path", r.URL.Path, "keyPrefix", apiKeyHeader[:min(len(apiKeyHeader), 10)]+"...")
				ac.apiKeyMiddleware.Auth()(next).ServeHTTP(w, r)
			} else if authHeader != "" {
				// Route to JWT authentication
				ac.logger.Debugw("Routing to JWT authentication", "ip", ac.trustedProxies.ClientIP(r), "path", r.URL.Path, "tokenPrefix", authHeader[:min(len(authHeader), 10)]+"...")
				ac.jwtMiddleware.Auth()(next).ServeHTTP(w, r)
			} else {
				// No authentication headers provided
				ac.logger.Warnw("Missing authentication headers", "ip", ac.trustedProxies.ClientIP(r), "path", r.URL.Path)
				httpx.JSON(w, http.StatusUnauthorized, utils.NewFailResponse("Authentication required: provide either X-API-Key header or Authorization header"))

				return
			}
		})
	}
}
