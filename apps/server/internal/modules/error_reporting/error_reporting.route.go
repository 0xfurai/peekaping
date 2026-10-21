package error_reporting

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

// ConnectRoute registers the error reporting routes behind the same authentication as the
// settings routes
func (r *Route) ConnectRoute(
	rg *httpx.Router,
	controller *Controller,
) {
	router := rg.Group("/error-reporting")

	router.Use(r.middleware.AllAuth())

	httpx.Register(router, "GET", "", huma.Operation{Summary: "Get the error reporting state", Description: "Whether an admin opted in to error reports, and whether the ERROR_REPORTING or DO_NOT_TRACK environment variable forces them off.", Tags: []string{"error-reporting"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusUnauthorized}}, controller.Get)
	httpx.Register(router, "PUT", "", huma.Operation{Summary: "Opt in or out of error reports", Description: "Error reports are crash reports: the error type, the stack frames, the release and the install's mode, storage backend, OS and architecture. In cluster mode the change reaches the producer, the worker and the ingester within a minute. The choice is stored while the environment forces error reports off, and the environment still wins.", Tags: []string{"error-reporting"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.Set)
}
