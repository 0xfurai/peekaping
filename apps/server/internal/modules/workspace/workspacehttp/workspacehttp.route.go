package workspacehttp

import (
	"net/http"

	"peekaping/internal/httpx"
	"peekaping/internal/limits"

	"github.com/danielgtaylor/huma/v2"
	"go.uber.org/dig"

	"peekaping/internal/utils"
)

// RouteAuth is the authentication of the routes: the middleware package provides it, and it can't
// be imported here, since it imports the modules that import this package.
type RouteAuth interface {
	// WorkspaceAuth is for a user or an API key, in the workspace the request names
	WorkspaceAuth() httpx.Middleware
	// WorkspaceJWTAuth is for a signed-in user only, in the workspace the request names
	WorkspaceJWTAuth() httpx.Middleware
	// JWTAuth is for a signed-in user, with no workspace
	JWTAuth() httpx.Middleware
}

type Route struct {
	controller *Controller
	middleware RouteAuth
}

func NewRoute(controller *Controller, middleware RouteAuth) *Route {
	return &Route{controller: controller, middleware: middleware}
}

func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewController, NewRoute)
}

// ConnectRoute registers, under /api/v1:
//
//   - PATCH /workspace, /workspace/members, /workspace/invites, /workspace/limits and
//     /workspace/leave: the name, the people and the limits of the workspace the request runs in
//     (X-Workspace-ID, else the user's default one)
//   - POST /workspaces: a new workspace for the signed-in user
//   - /invites/{token}: what a holder of an invite link sees, and accepting it
func (r *Route) ConnectRoute(router *httpx.Router, controller *Controller) {
	people := router.Group("workspace")
	people.Use(r.middleware.WorkspaceAuth())
	people.DocumentWorkspace()
	{
		httpx.Register(people, "PATCH", "", huma.Operation{Summary: "Rename the workspace", Description: "An admin or owner (or an API key) renames the workspace the request runs in. The name is trimmed and must be 1 to 255 characters. A member is refused (403).", Tags: []string{"workspace"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError}}, controller.RenameWorkspace)
		httpx.Register(people, "GET", "limits", huma.Operation{Summary: "Get the limits of the workspace", Description: "Every member of the workspace (and an API key) sees its limits: for each, its value (null for none), what the workspace has of it and where it comes from: the workspace's own limits, which an instance admin sets, the install's defaults (Admin → Settings, or WORKSPACE_LIMIT_*) or none. An install that sets no limits answers null for each. members counts the pending invites beside the members; owned_workspaces counts the caller's own workspaces (0 for an API key) against the install's defaults alone; min_check_interval is used as the shortest interval that applies, the stricter of MIN_CHECK_INTERVAL and the limit; retention_days as the workspace's data retention period.", Tags: []string{"workspace"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusInternalServerError}}, controller.GetLimits)
		httpx.Register(people, "GET", "members", huma.Operation{Summary: "List the members of the workspace", Description: "Every member of the workspace sees who is in it and with which role.", Tags: []string{"workspace"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusInternalServerError}}, controller.ListMembers)
		httpx.Register(people, "PATCH", "members/{userId}", huma.Operation{Summary: "Change a member's role", Description: "An admin or owner changes the role of a member or an admin. Only an owner makes an owner or changes one, and the last owner stays an owner (409).", Tags: []string{"workspace"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusInternalServerError}}, controller.ChangeRole)
		httpx.Register(people, "DELETE", "members/{userId}", huma.Operation{Summary: "Remove a member", Description: "An admin or owner removes a member or an admin. Only an owner removes an owner, and the last owner can't be removed (409). The removed user loses access to the workspace at once.", Tags: []string{"workspace"}, DefaultStatus: http.StatusNoContent, Security: httpx.SharedAuth, Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusInternalServerError}}, controller.RemoveMember)
		httpx.Register(people, "GET", "invites", huma.Operation{Summary: "List the invites of the workspace", Description: "The invites that were neither used nor revoked. An admin or owner sees them. They hold no token: that is shown once, when the invite is made.", Tags: []string{"workspace"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError}}, controller.ListInvites)
		httpx.Register(people, "POST", "invites", huma.Operation{Summary: "Invite someone to the workspace", Description: "An admin or owner invites an email with a role; only an owner invites an owner. The answer holds the invite's link, once. It works once, for 7 days, for the account with that email. The link is also emailed to the invited address when the install sends account emails (ACCOUNT_SMTP_HOST is set); otherwise copy it to the person you invite. An earlier invite for the same email is revoked. Each invite can send an email, so a caller (a user, or the API keys of a workspace together) makes at most 20 an hour and one address gets at most 3 an hour; past either the answer is 429. A caller that may not invite is refused before either limit counts. A workspace whose members and pending invites are as many as its members limit is refused (403 LIMIT_REACHED, limit members); inviting a pending address again replaces its invite and takes no more room.", Tags: []string{"workspace"}, DefaultStatus: http.StatusCreated, Security: httpx.SharedAuth, Responses: map[string]*huma.Response{"403": httpx.ErrorVariants[limits.ReachedResponse](people, 403)}, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusTooManyRequests, http.StatusInternalServerError}}, controller.CreateInvite)
		httpx.Register(people, "DELETE", "invites/{id}", huma.Operation{Summary: "Revoke an invite", Description: "An admin or owner revokes an invite that was neither used nor revoked; its link stops working.", Tags: []string{"workspace"}, DefaultStatus: http.StatusNoContent, Security: httpx.SharedAuth, Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError}}, controller.RevokeInvite)
	}

	// Leaving is a user's own act: an API key has no user, so it can't
	leaving := router.Group("workspace")
	leaving.Use(r.middleware.WorkspaceJWTAuth())
	leaving.DocumentWorkspace()
	httpx.Register(leaving, "POST", "leave", huma.Operation{Summary: "Leave the workspace", Description: "The signed-in user leaves the workspace and loses access to it at once. The last owner can't leave (409).", Tags: []string{"workspace"}, DefaultStatus: http.StatusNoContent, Security: httpx.JWTOnly, Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusInternalServerError}}, controller.Leave)

	// A new workspace belongs to the user, not to a workspace they are in
	workspaces := router.Group("workspaces")
	workspaces.Use(r.middleware.JWTAuth())
	httpx.Register(workspaces, "POST", "", huma.Operation{Summary: "Create a workspace", Description: "Any signed-in user makes a workspace and becomes its owner. It starts with the default settings of a new workspace. A user who owns as many workspaces as the install's default limit of owned workspaces (Admin → Settings, or WORKSPACE_LIMIT_OWNED_WORKSPACES) is refused (403 LIMIT_REACHED, limit owned_workspaces).", Tags: []string{"workspace"}, DefaultStatus: http.StatusCreated, Security: httpx.JWTOnly, Responses: map[string]*huma.Response{"403": httpx.ErrorVariants[limits.ReachedResponse](workspaces, 403)}, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError}}, controller.CreateWorkspace)

	invites := router.Group("invites")
	httpx.Register(invites, "GET", "{token}", huma.Operation{Summary: "Look at an invite", Description: "What the holder of an invite link may see: the workspace's name, the invited email, the role and whether the invite can still be used. It needs no sign-in.", Tags: []string{"workspace"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusNotFound, http.StatusInternalServerError}}, controller.PreviewInvite)
	invites.Use(r.middleware.JWTAuth())
	httpx.Register(invites, "POST", "{token}/accept", huma.Operation{Summary: "Accept an invite", Description: "A signed-in user whose account email is the invited one joins the workspace with the invite's role. A used, expired or revoked invite is refused (410), and so is one for another email (403). Someone without an account signs up through the invite instead (POST /auth/register with inviteToken). An invite counted against the members limit when it was made can be accepted; a workspace that has as many members as its limit refuses it (403 LIMIT_REACHED, limit members).", Tags: []string{"workspace"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Responses: map[string]*huma.Response{"403": httpx.ErrorVariants[limits.ReachedResponse](invites, 403)}, Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusInternalServerError}}, controller.AcceptInvite)
}
