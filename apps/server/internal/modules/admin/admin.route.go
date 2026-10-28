package admin

import (
	"net/http"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/middleware"

	"github.com/danielgtaylor/huma/v2"
)

type Route struct {
	controller *Controller
	middleware *middleware.AuthChain
}

func NewRoute(controller *Controller, middleware *middleware.AuthChain) *Route {
	return &Route{controller: controller, middleware: middleware}
}

// ConnectRoute registers the admin area under /api/v1/admin. Every route is for an instance admin
// signed in as a user: any other user and any API key get 403 (docs/adr/0005 section 10).
func (r *Route) ConnectRoute(router *httpx.Router, controller *Controller) {
	admin := router.Group("admin")
	admin.Use(r.middleware.InstanceAdminAuth())
	forbidden := []int{http.StatusUnauthorized, http.StatusForbidden}
	statuses := func(more ...int) []int { return append(append([]int{}, forbidden...), more...) }
	httpx.Register(admin, "GET", "users", huma.Operation{Summary: "List the users of the install", Description: "Only an instance admin may do this: any other user and any API key get 403. Every user with their email, when they signed up and last signed in, whether their email is verified, whether they are an instance admin, whether they are disabled, whether they have 2FA on, and their workspaces with their role in each. q finds the users whose email contains it, ignoring case. The oldest user is first.", Tags: []string{"admin"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: statuses(http.StatusBadRequest, http.StatusInternalServerError)}, controller.ListUsers)
	httpx.Register(admin, "PATCH", "users/{id}", huma.Operation{Summary: "Change a user", Description: "Only an instance admin may do this. instanceAdmin gives or takes the instance admin flag: the last instance admin can't lose it (409). active false disables the user: they can't sign in, their sessions end at once and their workspaces keep running; an admin can't disable themselves (409). active true enables them again. A field left out stays as it is.", Tags: []string{"admin"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: statuses(http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusInternalServerError)}, controller.UpdateUser)
	httpx.Register(admin, "POST", "users/{id}/reset-link", huma.Operation{Summary: "Make a password reset link for a user", Description: "Only an instance admin may do this. The answer holds a single-use link to the web app's reset page, once: copy it to the user. It works without a sender for account emails, expires in an hour, and ends all the user's sessions when it is used.", Tags: []string{"admin"}, DefaultStatus: http.StatusCreated, Security: httpx.JWTOnly, Errors: statuses(http.StatusNotFound, http.StatusInternalServerError)}, controller.CreateResetLink)
	httpx.Register(admin, "POST", "users/{id}/disable-2fa", huma.Operation{Summary: "Turn off a user's 2FA", Description: "Only an instance admin may do this. The user signs in with their password alone, and can set 2FA up again.", Tags: []string{"admin"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: statuses(http.StatusNotFound, http.StatusInternalServerError)}, controller.DisableTwoFA)
	httpx.Register(admin, "DELETE", "users/{id}", huma.Operation{Summary: "Delete a user", Description: "Only an instance admin may do this. The user's memberships go with them. It is refused (409) for yourself, for the last instance admin, and while the user is the last owner of a workspace: that answer is {\"error\":{\"code\":\"LAST_OWNER\",\"workspaces\":[{\"id\",\"name\"}]}}, listing the workspaces whose only owner the user is. The other refusals answer with a message.", Responses: map[string]*huma.Response{"409": httpx.ErrorVariants[LastOwnerErrorResponse](admin, 409)}, Tags: []string{"admin"}, DefaultStatus: http.StatusNoContent, Security: httpx.JWTOnly, Errors: statuses(http.StatusNotFound, http.StatusConflict, http.StatusInternalServerError)}, controller.DeleteUser)
}
