package status_page

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

func NewRoute(controller *Controller, middleware *middleware.AuthChain) *Route {
	return &Route{
		controller: controller,
		middleware: middleware,
	}
}

// unavailableNote is what a public route says of the 503 it answers for a suspended workspace
const unavailableNote = "Answers 503 with the code STATUS_PAGE_UNAVAILABLE and the message \"This status page is unavailable\" when the page is published and its workspace is suspended, whether by an instance admin or while it waits for its owner to verify their email address. It does not say which, and a page that is not published is still 404."

func (r *Route) ConnectRoute(rg *httpx.Router, controller *Controller) {
	// Public routes
	sp := rg.Group("status-pages")
	// A public page of a suspended workspace answers 503 and says nothing of why (PP-372). Each
	// operation gets a map of its own: registering one adds its 200 to the map, and a shared map
	// would give every later operation the first one's 200.
	unavailable := func() map[string]*huma.Response {
		return map[string]*huma.Response{"503": httpx.ErrorResponse[UnavailableResponse](sp, http.StatusServiceUnavailable)}
	}
	httpx.Register(sp, "GET", "/slug/{slug}", huma.Operation{Summary: "Get a status page by slug", Description: unavailableNote, Responses: unavailable(), Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusInternalServerError, http.StatusNotFound}}, r.controller.FindBySlug)
	httpx.Register(sp, "GET", "/domain/{domain}", huma.Operation{Summary: "Get a status page by domain name", Description: unavailableNote, Responses: unavailable(), Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusInternalServerError, http.StatusNotFound}}, r.controller.FindByDomain)
	httpx.Register(sp, "GET", "/slug/{slug}/monitors", huma.Operation{Summary: "Get monitors for a status page by slug with heartbeats and uptime", Description: unavailableNote, Responses: unavailable(), Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusInternalServerError, http.StatusNotFound}}, r.controller.GetMonitorsBySlug)
	httpx.Register(sp, "GET", "/slug/{slug}/monitors/homepage", huma.Operation{Summary: "Get monitors for a status page by slug for homepage", Description: unavailableNote, Responses: unavailable(), Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusInternalServerError, http.StatusNotFound}}, r.controller.GetMonitorsBySlugForHomepage)

	sp.Use(r.middleware.WorkspaceAuth())
	sp.DocumentWorkspace()
	{
		httpx.Register(sp, "POST", "", huma.Operation{Summary: "Create a new status page", Responses: map[string]*huma.Response{"400": httpx.ErrorVariants2[DomainErrorResponse, SlugErrorResponse](sp, 400), "403": httpx.ErrorVariants[limits.ReachedResponse](sp, 403)}, Tags: []string{"status_page"}, DefaultStatus: http.StatusCreated, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusForbidden, http.StatusInternalServerError, http.StatusUnauthorized}}, r.controller.Create)
		httpx.Register(sp, "GET", "", huma.Operation{Summary: "Get all status pages", Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, r.controller.FindAll)
		httpx.Register(sp, "GET", "/{id}", huma.Operation{Summary: "Get a status page by ID", Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusUnauthorized}}, r.controller.FindByID)
		httpx.Register(sp, "PATCH", "/{id}", huma.Operation{Summary: "Update a status page", Responses: map[string]*huma.Response{"400": httpx.ErrorVariants2[DomainErrorResponse, SlugErrorResponse](sp, 400), "403": httpx.ErrorVariants[limits.ReachedResponse](sp, 403)}, Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusBadRequest, http.StatusForbidden, http.StatusInternalServerError, http.StatusUnauthorized}}, r.controller.Update)
		httpx.Register(sp, "DELETE", "/{id}", huma.Operation{Summary: "Delete a status page", Tags: []string{"status_page"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, r.controller.Delete)
	}
}
