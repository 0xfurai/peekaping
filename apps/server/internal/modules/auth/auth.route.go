package auth

import (
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/limits"
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
	httpx.Register(auth, "POST", "/register", huma.Operation{Summary: "Register new admin", Description: "Who may register depends on SIGNUP_MODE. In invite mode (the default) it creates the install's first user, who is its instance admin and the owner of its first workspace, and refuses once the install has a user (400). In open mode anyone may register: it creates the user and a workspace of their own, \"My workspace\", that they own. Only the install's very first user is an instance admin. The new workspace is suspended until the user verifies their email address (GET /me says so), and the verification email is sent. In open mode a client address may register 5 times an hour (429), an email that has an account is refused (409), and when the install uses Cloudflare Turnstile the body needs turnstileToken, which the server checks: a missing, refused or unchecked answer is 400. With inviteToken, the token of an invite link, it creates the user the invite names instead: they join the inviting workspace with the invite's role, get no workspace of their own and no instance admin flag. The email must be the invited one (403), and the invite must exist (404) and not be used, expired or revoked (410). An email that has an account already is refused (409): sign in and accept the invite. A workspace that has as many members as its limit refuses it (403 LIMIT_REACHED, limit members), and no account is made.", Responses: map[string]*huma.Response{"403": httpx.ErrorVariants[limits.ReachedResponse](auth, 403)}, Tags: []string{"auth"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusTooManyRequests, http.StatusInternalServerError}}, controller.Register)

	// Two lockouts guard the login. The body limit comes first, so that no guard reads more than
	// a login needs and a body over it is refused with 413 before the handler sees it. The address
	// guard is next, so a request it blocks doesn't count against the account. The account guard
	// counts failures whatever the address.
	httpx.Register(auth, "POST", "/login", huma.Operation{Summary: "Login admin", Responses: map[string]*huma.Response{"429": lockout}, Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusRequestEntityTooLarge, http.StatusTooManyRequests}}, controller.Login, httpx.LimitBody(maxLoginBody), r.bruteforceGuard.Middleware(), r.accountGuard.Middleware())

	httpx.Register(auth, "POST", "/refresh", huma.Operation{Summary: "Refresh access token", Description: "Refuses a refresh token issued before the user's last password change or password reset.", Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized}}, controller.RefreshToken)

	// Account emails. They are public: a visitor who forgot the password has no session, and a
	// link in an email is authenticated by its token.
	httpx.Register(auth, "GET", "/config", huma.Operation{Summary: "Get the install's account email setting", Description: "Whether the install sends account emails (a password reset link, an email verification link and emailed invites): accountEmails is true when ACCOUNT_SMTP_HOST is set. When it is false the app offers none of them. signupMode is SIGNUP_MODE (invite or open), signupOpen is whether to offer a sign-up form (always in open mode, and in invite mode while the install has no user), firstSignup is whether the install has no user yet in either mode (the next signup becomes its instance admin), and turnstileSiteKey is the public key of the Turnstile widget that open signup shows, or null.", Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusInternalServerError}}, controller.AuthConfig)
	httpx.Register(auth, "POST", "/forgot-password", huma.Operation{Summary: "Ask for a password reset link", Description: "Emails a single-use link that resets the password and expires in one hour, if an account has the email. The answer is 202 with the same body whether it does or not, so it does not tell which addresses have accounts. At most 5 emails per hour go to one address (the request is answered the same and sends nothing past that), and a client address that asks more than 20 times an hour gets 429. 404 when the install sends no account emails.", Tags: []string{"auth"}, DefaultStatus: http.StatusAccepted, Errors: []int{http.StatusBadRequest, http.StatusNotFound, http.StatusTooManyRequests, http.StatusInternalServerError}}, controller.ForgotPassword, httpx.LimitBody(maxLoginBody))
	httpx.Register(auth, "POST", "/reset-password", huma.Operation{Summary: "Reset the password with a reset link's token", Description: "Sets the new password (the signup rules apply), marks the token used and ends every session of the user: refresh tokens and access tokens issued before are refused. The user's other unused reset links stop working. A token that is unknown, used or expired answers 400. At most 20 requests per hour from one client address; more answer 429.", Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusTooManyRequests, http.StatusInternalServerError}}, controller.ResetPassword, httpx.LimitBody(maxLoginBody))
	httpx.Register(auth, "POST", "/verify-email", huma.Operation{Summary: "Verify an email address with a verification link's token", Description: "Records that the account's address was verified and marks the token used. A token that is unknown, used or expired answers 400. At most 20 requests per hour from one client address; more answer 429.", Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusTooManyRequests, http.StatusInternalServerError}}, controller.VerifyEmail, httpx.LimitBody(maxLoginBody))

	auth.Use(r.middleware.Auth())
	httpx.Register(auth, "POST", "/2fa/setup", huma.Operation{Summary: "Enable 2FA (TOTP) for user", Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized}}, controller.SetupTwoFA)
	httpx.Register(auth, "POST", "/2fa/verify", huma.Operation{Summary: "Verify 2FA (TOTP) code for user", Responses: map[string]*huma.Response{"400": httpx.ErrorVariants[TwoFAVerifyResponseDto](auth, 400)}, Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized}}, controller.VerifyTwoFA)
	httpx.Register(auth, "POST", "/2fa/disable", huma.Operation{Summary: "Disable 2FA (TOTP) for user", Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized}}, controller.DisableTwoFA)
	httpx.Register(auth, "PUT", "/password", huma.Operation{Summary: "Update user password", Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusUnauthorized, http.StatusBadRequest, http.StatusInternalServerError}}, controller.UpdatePassword)

	httpx.Register(auth, "POST", "/verify-email/resend", huma.Operation{Summary: "Send the verification link again", Description: "Emails the signed-in user a new verification link; the earlier ones stop working. 409 when the address is verified already, 404 when the install sends no account emails, and 429 past 5 requests per hour for the address or 20 per client address.", Tags: []string{"auth"}, DefaultStatus: http.StatusAccepted, Security: httpx.JWTOnly, Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusTooManyRequests, http.StatusInternalServerError}}, controller.ResendVerification)

	// The signed-in user and their workspaces. It is not a workspace route: it names no workspace,
	// so that a user in none can still read it.
	me := router.Group("/me")
	me.Use(r.middleware.Auth())
	httpx.Register(me, "GET", "", huma.Operation{Summary: "Get the signed-in user and their workspaces", Description: "The user, the workspaces they are a member of with their role in each, and the default workspace: the one they joined first, which a request without X-Workspace-ID runs in. A user in no workspace gets an empty list and a null default.", Tags: []string{"auth"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusUnauthorized, http.StatusInternalServerError}}, controller.Me)
}
