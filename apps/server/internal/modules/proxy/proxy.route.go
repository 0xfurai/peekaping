package proxy

import (
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/middleware"
	"peekaping/internal/utils"
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

func (uc *Route) ConnectRoute(
	rg *httpx.Router,
	controller *Controller,
) {
	router := rg.Group("proxies")

	router.Use(uc.middleware.WorkspaceAuth())
	router.DocumentWorkspace()
	httpx.Register(router, "GET", "", httpx.Operation{Summary: "Get proxies", Tags: []string{"proxy"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.controller.FindAll)
	httpx.Register(router, "POST", "", httpx.Operation{Summary: "Create proxy", Responses: map[string]*httpx.Response{"400": httpx.ErrorVariants[utils.TargetNotAllowedResponse]()}, Tags: []string{"proxy"}, DefaultStatus: http.StatusCreated, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.controller.Create)
	httpx.Register(router, "GET", "{id}", httpx.Operation{Summary: "Get proxy by ID", Tags: []string{"proxy"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusUnauthorized}}, uc.controller.FindByID)
	httpx.Register(router, "PUT", "{id}", httpx.Operation{Summary: "Update proxy", Responses: map[string]*httpx.Response{"400": httpx.ErrorVariants[utils.TargetNotAllowedResponse]()}, Tags: []string{"proxy"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.controller.UpdateFull)
	httpx.Register(router, "PATCH", "{id}", httpx.Operation{Summary: "Update proxy", Responses: map[string]*httpx.Response{"400": httpx.ErrorVariants[utils.TargetNotAllowedResponse]()}, Tags: []string{"proxy"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.controller.UpdatePartial)
	httpx.Register(router, "DELETE", "{id}", httpx.Operation{Summary: "Delete proxy", Tags: []string{"proxy"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.controller.Delete)
}
