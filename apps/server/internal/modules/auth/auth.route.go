package auth

import (
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/bruteforce"

	"github.com/danielgtaylor/huma/v2"
)

// maxLoginBody is the most a login request may send, 64 KiB: a login is an email and a password.
const maxLoginBody = 64 << 10

type Route struct {
	controller      *Controller
	middleware      *MiddlewareProvider
	bruteforceGuard *bruteforce.Guard
	accountGuard    *bruteforce.AccountGuard
}

func NewRoute(
	controller *Controller,
	middleware *MiddlewareProvider,
	bruteforceGuard *bruteforce.Guard,
	accountGuard *bruteforce.AccountGuard,
) *Route {
	return &Route{
		controller,
		middleware,
		bruteforceGuard,
		accountGuard,
	}
}

func (r *Route) ConnectRoute(router *httpx.Router, controller *Controller) {
	auth := router.Group("/auth")
	lockout := httpx.ErrorResponse[bruteforce.BlockResponse](auth, http.StatusTooManyRequests)
	lockout.Headers = map[string]*huma.Param{"Retry-After": {Description: "Seconds until the login lockout expires.", Schema: &huma.Schema{Type: "integer"}}}
	httpx.Register(auth, "POST", "/register", huma.Operation{Summary: "Register new admin", Tags: []string{"auth"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError}}, controller.Register)

	// Two lockouts guard the login. The body limit comes first, so that no guard reads more than
	// a login needs and a body over it is refused with 413 before the handler sees it. The address
	// guard is next, so a request it blocks doesn't count against the account. The account guard
	// counts failures whatever the address.
	httpx.Register(auth, "POST", "/login", huma.Operation{Summary: "Login admin", Responses: map[string]*huma.Response{"429": lockout}, Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusRequestEntityTooLarge, http.StatusTooManyRequests}}, controller.Login, httpx.LimitBody(maxLoginBody), r.bruteforceGuard.Middleware(), r.accountGuard.Middleware())

	httpx.Register(auth, "POST", "/refresh", huma.Operation{Summary: "Refresh access token", Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized}}, controller.RefreshToken)

	auth.Use(r.middleware.Auth())
	httpx.Register(auth, "POST", "/2fa/setup", huma.Operation{Summary: "Enable 2FA (TOTP) for user", Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized}}, controller.SetupTwoFA)
	httpx.Register(auth, "POST", "/2fa/verify", huma.Operation{Summary: "Verify 2FA (TOTP) code for user", Responses: map[string]*huma.Response{"400": httpx.ErrorVariants[TwoFAVerifyResponseDto](auth, 400)}, Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized}}, controller.VerifyTwoFA)
	httpx.Register(auth, "POST", "/2fa/disable", huma.Operation{Summary: "Disable 2FA (TOTP) for user", Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized}}, controller.DisableTwoFA)
	httpx.Register(auth, "PUT", "/password", huma.Operation{Summary: "Update user password", Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusUnauthorized, http.StatusBadRequest, http.StatusInternalServerError}}, controller.UpdatePassword)

	// The signed-in user and their workspaces. It is not a workspace route: it names no workspace,
	// so that a user in none can still read it.
	me := router.Group("/me")
	me.Use(r.middleware.Auth())
	httpx.Register(me, "GET", "", huma.Operation{Summary: "Get the signed-in user and their workspaces", Description: "The user, the workspaces they are a member of with their role in each, and the default workspace: the one they joined first, which a request without X-Workspace-ID runs in. A user in no workspace gets an empty list and a null default.", Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusUnauthorized, http.StatusInternalServerError}}, controller.Me)
}
