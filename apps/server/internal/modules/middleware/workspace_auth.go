package middleware

import (
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/utils"
	"strings"

	"github.com/google/uuid"
)

// workspaceHeader names the workspace a request runs in (docs/adr/0005 section 7)
const workspaceHeader = "X-Workspace-ID"

// MARK: WorkspaceAuth

// WorkspaceAuth is AllAuth for a route whose records belong to a workspace. After the request is
// authenticated it resolves the workspace the request runs in and puts it, with the caller's role
// there, into the context (see workspace.FromContext):
//
//   - An API key runs in its own workspace, as an admin. A request that names another workspace
//     answers 404.
//   - A JWT runs in the workspace the X-Workspace-ID header names, if the user is a member of it,
//     and in the user's default workspace, the one they joined first, without the header. A
//     workspace the user isn't in, an ID that names none and a header that isn't a UUID answer
//     404, and so does a user in no workspace.
func (ac *AuthChain) WorkspaceAuth() httpx.Middleware {
	return ac.inWorkspace(ac.AllAuth())
}

// WorkspaceJWTAuth is WorkspaceAuth for a route that only a signed-in user may reach, not an API key
func (ac *AuthChain) WorkspaceJWTAuth() httpx.Middleware {
	return ac.inWorkspace(ac.jwtOnly)
}

// jwtOnly is the JWT middleware. It is built per request, as AllAuth does, so that the routes can
// be registered with no middleware behind them to document the API.
func (ac *AuthChain) jwtOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac.jwtMiddleware.Auth()(next).ServeHTTP(w, r)
	})
}

func (ac *AuthChain) inWorkspace(authenticate httpx.Middleware) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, role, ok := ac.resolveWorkspace(w, r)
			if !ok {
				return
			}
			next.ServeHTTP(w, r.WithContext(workspace.WithContext(r.Context(), id, role)))
		}))
	}
}

// resolveWorkspace answers the request itself and returns false when it runs in no workspace
func (ac *AuthChain) resolveWorkspace(w http.ResponseWriter, r *http.Request) (workspace.ID, workspace.Role, bool) {
	named, hasHeader := r.Header.Get(workspaceHeader), r.Header.Get(workspaceHeader) != ""
	if hasHeader && !isCanonicalUUID(named) {
		ac.notFound(w, r)
		return "", "", false
	}
	named = strings.ToLower(named)

	if httpx.Value(r.Context(), "authType") == "api_key" {
		own, _ := httpx.Value(r.Context(), "apiKeyWorkspaceId").(workspace.ID)
		if own == "" || (hasHeader && named != strings.ToLower(string(own))) {
			ac.notFound(w, r)
			return "", "", false
		}
		return own, workspace.RoleAdmin, true
	}

	userID, _ := httpx.Value(r.Context(), "userId").(string)
	places, err := ac.workspaces.ListMemberships(r.Context(), userID)
	if err != nil {
		ac.logger.Errorw("Failed to list the workspaces of a user", "userId", userID, "error", err)
		httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return "", "", false
	}
	// The default workspace is the first of the list, the one the user joined first
	for _, place := range places {
		if !hasHeader || strings.ToLower(string(place.Workspace.ID)) == named {
			return place.Workspace.ID, place.Role, true
		}
	}
	ac.notFound(w, r)
	return "", "", false
}

func (ac *AuthChain) notFound(w http.ResponseWriter, r *http.Request) {
	ac.logger.Debugw("Workspace not found for the request", "path", r.URL.Path)
	httpx.JSON(w, http.StatusNotFound, utils.NewFailResponse("Workspace not found"))
}

// isCanonicalUUID is whether s is a UUID in its usual form, 36 characters with hyphens. The
// parser takes more shapes than that, and a workspace ID has one.
func isCanonicalUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil && len(s) == 36
}

// MARK: InstanceAdminAuth

// InstanceAdminAuth is for a route that manages the install and not a workspace (docs/adr/0005
// section 10). It takes a signed-in user's JWT, as the routes it guards always did, and answers 403
// unless the user is an instance admin. The flag is read on every request, since it can change
// while a token is still valid. An API key belongs to a workspace and is never an instance admin.
func (ac *AuthChain) InstanceAdminAuth() httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return ac.jwtOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			forbidden := func() {
				httpx.JSON(w, http.StatusForbidden, utils.NewFailResponse("Only an instance admin may do this"))
			}
			if httpx.Value(r.Context(), "authType") != "jwt" {
				forbidden()
				return
			}
			userID, _ := httpx.Value(r.Context(), "userId").(string)
			user, err := ac.users.FindByID(r.Context(), userID)
			if err != nil {
				ac.logger.Errorw("Failed to read the instance admin flag of a user", "userId", userID, "error", err)
				httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
				return
			}
			if user == nil || !user.InstanceAdmin {
				forbidden()
				return
			}
			next.ServeHTTP(w, r)
		}))
	}
}
