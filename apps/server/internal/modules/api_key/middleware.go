package api_key

import (
	"errors"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/utils"
	"strings"
)

// workspaceHeader names the workspace a request runs in (docs/adr/0005 section 7)
const workspaceHeader = "X-Workspace-ID"

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
func (p *MiddlewareProvider) Auth() httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get the X-API-Key header
			authHeader := r.Header.Get("X-API-Key")
			if authHeader == "" {
				httpx.JSON(w, http.StatusUnauthorized, utils.NewFailResponse("X-API-Key header is required"))

				return
			}

			// Only accept API keys
			if !strings.HasPrefix(authHeader, ApiKeyPrefix) {
				httpx.JSON(w, http.StatusUnauthorized, utils.NewFailResponse("API key required"))

				return
			}

			// Validate the API key
			ctx := r.Context()
			if named := r.Header.Get(workspaceHeader); named != "" {
				ctx = WithRequestedWorkspace(ctx, named)
			}
			apiKey, err := p.service.ValidateKey(ctx, authHeader)
			if errors.Is(err, ErrWorkspaceMismatch) {
				// The same answer as the workspace middleware gives, which a request never reaches
				httpx.JSON(w, http.StatusNotFound, utils.NewFailResponse("Workspace not found"))

				return
			}
			if err != nil {
				httpx.JSON(w, http.StatusUnauthorized, utils.NewFailResponse("Invalid or expired API key"))

				return
			}

			// Set API key information in the context
			r = httpx.WithValue(r, "apiKeyId", apiKey.ID)
			// The key belongs to one workspace and runs in it. The workspace middleware that follows
			// resolves the request's workspace from this.
			r = httpx.WithValue(r, "apiKeyWorkspaceId", apiKey.WorkspaceID)
			r = httpx.WithValue(r, "authType", "api_key")

			next.ServeHTTP(w, r)
		})
	}
}
