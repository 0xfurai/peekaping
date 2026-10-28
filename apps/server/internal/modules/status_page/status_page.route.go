package status_page

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

func NewRoute(controller *Controller, middleware *middleware.AuthChain) *Route {
	return &Route{
		controller: controller,
		middleware: middleware,
	}
}

func (r *Route) ConnectRoute(rg *httpx.Router, controller *Controller) {
	// Public routes
	sp := rg.Group("status-pages")
	httpx.Register(sp, "GET", "/slug/{slug}", huma.Operation{Summary: "Get a status page by slug", Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusInternalServerError, http.StatusNotFound}}, r.controller.FindBySlug)
	httpx.Register(sp, "GET", "/domain/{domain}", huma.Operation{Summary: "Get a status page by domain name", Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusInternalServerError, http.StatusNotFound}}, r.controller.FindByDomain)
	httpx.Register(sp, "GET", "/slug/{slug}/monitors", huma.Operation{Summary: "Get monitors for a status page by slug with heartbeats and uptime", Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusInternalServerError, http.StatusNotFound}}, r.controller.GetMonitorsBySlug)
	httpx.Register(sp, "GET", "/slug/{slug}/monitors/homepage", huma.Operation{Summary: "Get monitors for a status page by slug for homepage", Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusInternalServerError, http.StatusNotFound}}, r.controller.GetMonitorsBySlugForHomepage)

	sp.Use(r.middleware.WorkspaceAuth())
	sp.Document(httpx.WorkspaceIDHeader)
	{
		httpx.Register(sp, "POST", "", huma.Operation{Summary: "Create a new status page", Responses: map[string]*huma.Response{"400": httpx.ErrorVariants[DomainErrorResponse](sp, 400)}, Tags: []string{"status_page"}, DefaultStatus: http.StatusCreated, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, r.controller.Create)
		httpx.Register(sp, "GET", "", huma.Operation{Summary: "Get all status pages", Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, r.controller.FindAll)
		httpx.Register(sp, "GET", "/{id}", huma.Operation{Summary: "Get a status page by ID", Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusUnauthorized}}, r.controller.FindByID)
		httpx.Register(sp, "PATCH", "/{id}", huma.Operation{Summary: "Update a status page", Responses: map[string]*huma.Response{"400": httpx.ErrorVariants[DomainErrorResponse](sp, 400)}, Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, r.controller.Update)
		httpx.Register(sp, "DELETE", "/{id}", huma.Operation{Summary: "Delete a status page", Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, r.controller.Delete)
	}
}
