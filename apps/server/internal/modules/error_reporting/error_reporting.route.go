package error_reporting

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

// ConnectRoute registers the error reporting routes. Reading the state takes the same
// authentication as the settings routes, a JWT or an API key. Opting in or out is the admin's
// choice (ADR 0003), so it takes a signed-in admin's JWT: an API key can't change it, the way it
// can't manage API keys.
func (r *Route) ConnectRoute(
	rg *httpx.Router,
	controller *Controller,
) {
	router := rg.Group("/error-reporting")

	httpx.Register(router, "GET", "", huma.Operation{Summary: "Get the error reporting state", Description: "Whether error reports are on (they are, until an admin opts out), and whether the ERROR_REPORTING environment variable forces them off.", Tags: []string{"error-reporting"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusUnauthorized}}, controller.Get, r.middleware.AllAuth())
	httpx.Register(router, "PUT", "", huma.Operation{Summary: "Opt in or out of error reports", Description: "Error reports are crash reports: the error type, the stack frames, the release and the install's mode, storage backend, OS and architecture. In cluster mode a change made on one api replica reaches the others within a minute. The choice is stored while the environment forces error reports off, and the environment still wins. Any choice also marks the one-time usage ping and error reports notice as seen.", Tags: []string{"error-reporting"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.Set, r.jwt.Auth())
}
