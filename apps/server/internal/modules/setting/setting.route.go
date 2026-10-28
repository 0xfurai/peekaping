package setting

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
		controller, middleware,
	}
}

func (uc *Route) ConnectRoute(
	rg *httpx.Router,
	controller *Controller,
) {
	router := rg.Group("/settings")

	router.Use(uc.middleware.WorkspaceAuth())

	httpx.Register(router, "GET", "key/{key}", huma.Operation{Summary: "Get setting by key", Tags: []string{"setting"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusUnauthorized}}, uc.controller.GetByKey)
	httpx.Register(router, "PUT", "key/{key}", huma.Operation{Summary: "Set setting by key", Description: "Only KEEP_DATA_PERIOD_DAYS can be set, with type int and a whole number of days from 1 to 36500. Every other key returns 404.", Tags: []string{"setting"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.controller.SetByKey)
	httpx.Register(router, "DELETE", "key/{key}", huma.Operation{Summary: "Delete setting by key", Tags: []string{"setting"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.controller.DeleteByKey)
}
