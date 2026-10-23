package probe

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

func (r *Route) ConnectRoute(router *httpx.Router, controller *Controller) {
	probes := router.Group("probes")

	// The admin's JWT or an API key
	probes.Use(r.middleware.AllAuth())
	{
		httpx.Register(probes, "POST", "", huma.Operation{Summary: "Create probe", Tags: []string{"probe"}, DefaultStatus: http.StatusCreated, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusNotImplemented, http.StatusUnauthorized}}, controller.CreateProbe)
		httpx.Register(probes, "GET", "", huma.Operation{Summary: "Get probes", Tags: []string{"probe"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusUnauthorized}}, controller.GetProbes)
		httpx.Register(probes, "GET", "{id}", huma.Operation{Summary: "Get probe", Tags: []string{"probe"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusNotImplemented, http.StatusUnauthorized}}, controller.GetProbe)
		httpx.Register(probes, "PUT", "{id}", huma.Operation{Summary: "Update probe", Tags: []string{"probe"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusInternalServerError, http.StatusNotImplemented, http.StatusUnauthorized}}, controller.UpdateProbe)
		httpx.Register(probes, "DELETE", "{id}", huma.Operation{Summary: "Delete probe", Tags: []string{"probe"}, DefaultStatus: http.StatusNoContent, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusInternalServerError, http.StatusNotImplemented, http.StatusUnauthorized}}, controller.DeleteProbe)
		httpx.Register(probes, "POST", "{id}/rotate-token", huma.Operation{Summary: "Rotate probe token", Tags: []string{"probe"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusInternalServerError, http.StatusNotImplemented, http.StatusUnauthorized}}, controller.RotateProbeToken)
	}
}
