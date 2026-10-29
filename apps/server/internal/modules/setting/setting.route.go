package setting

import (
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/limits"
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
	router.Document(httpx.WorkspaceIDHeader)

	httpx.Register(router, "GET", "key/{key}", huma.Operation{Summary: "Get setting by key", Description: "A setting of the request's workspace; any member can read it.", Tags: []string{"setting"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusUnauthorized}}, uc.controller.GetByKey)
	httpx.Register(router, "PUT", "key/{key}", huma.Operation{Summary: "Set setting by key", Description: "Only KEEP_DATA_PERIOD_DAYS can be set, with type int and a whole number of days from 1 to 36500. Every other key returns 404. It is a setting of the request's workspace: members read it, admins and owners change it, and a member's change returns 403. A period longer than the workspace's retention_days limit is refused (403 LIMIT_REACHED, limit retention_days); one at the limit is saved.", Responses: map[string]*huma.Response{"403": httpx.ErrorVariants[limits.ReachedResponse](router, 403)}, Tags: []string{"setting"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized, http.StatusForbidden}}, uc.controller.SetByKey)
	httpx.Register(router, "DELETE", "key/{key}", huma.Operation{Summary: "Delete setting by key", Description: "Admins and owners of the request's workspace can delete it; a member gets 403.", Tags: []string{"setting"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized, http.StatusForbidden}}, uc.controller.DeleteByKey)
}
