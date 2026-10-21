package telemetry

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

func NewRoute(
	controller *Controller,
	middleware *middleware.AuthChain,
) *Route {
	return &Route{
		controller,
		middleware,
	}
}

// ConnectRoute registers the telemetry routes behind the same authentication as the settings
// routes
func (r *Route) ConnectRoute(
	rg *httpx.Router,
	controller *Controller,
) {
	router := rg.Group("/telemetry")

	router.Use(r.middleware.AllAuth())

	httpx.Register(router, "GET", "", huma.Operation{Summary: "Get the usage ping state", Description: "Whether an admin opted in to the anonymous usage ping, whether the TELEMETRY or DO_NOT_TRACK environment variable forces it off, and the exact payload the next ping would send.", Tags: []string{"telemetry"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusUnauthorized}}, controller.Get)
	httpx.Register(router, "PUT", "", huma.Operation{Summary: "Opt in or out of the usage ping", Description: "Opting in creates a new random instance ID unless there is one. Opting out deletes it, so opting in again gives a new one. The choice is stored while the environment forces the ping off, and the environment still wins.", Tags: []string{"telemetry"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.Set)
}
