package telemetry

import (
	"net/http"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/middleware"

	"github.com/danielgtaylor/huma/v2"
)

type Route struct {
	controller *Controller
	middleware *middleware.AuthChain
	jwt        *auth.MiddlewareProvider
}

func NewRoute(
	controller *Controller,
	middleware *middleware.AuthChain,
	jwt *auth.MiddlewareProvider,
) *Route {
	return &Route{
		controller,
		middleware,
		jwt,
	}
}

// ConnectRoute registers the telemetry routes. Reading the state takes the same authentication as
// the settings routes, a JWT or an API key. Opting in or out is the admin's choice (ADR 0003), so
// it takes a signed-in admin's JWT: an API key can't change it, the way it can't manage API keys.
func (r *Route) ConnectRoute(
	rg *httpx.Router,
	controller *Controller,
) {
	router := rg.Group("/telemetry")

	httpx.Register(router, "GET", "", huma.Operation{Summary: "Get the usage ping state", Description: "Whether the anonymous usage ping is on (it is, until an admin opts out), whether the TELEMETRY environment variable forces it off, whether the one-time notice about the ping and error reports is still to be shown, and the exact payload the next ping would send.", Tags: []string{"telemetry"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusUnauthorized}}, controller.Get, r.middleware.AllAuth())
	httpx.Register(router, "PUT", "", huma.Operation{Summary: "Opt in or out of the usage ping", Description: "Opting in creates a new random instance ID unless there is one. Opting out deletes it, so opting in again gives a new one. The choice is stored while the environment forces the ping off, and the environment still wins. Any choice also marks the one-time notice as seen.", Tags: []string{"telemetry"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.Set, r.jwt.Auth())
	httpx.Register(router, "POST", "/notice", huma.Operation{Summary: "Dismiss the usage ping and error reports notice", Description: "An install that upgraded to a version with the usage ping and error reports on by default shows its admin a one-time notice. This marks it seen so it stops showing. It is not a choice: both features stay as they were. Making a choice with PUT /telemetry or PUT /error-reporting marks it seen too.", Tags: []string{"telemetry"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusInternalServerError, http.StatusUnauthorized}}, controller.MarkNotice, r.jwt.Auth())
}
