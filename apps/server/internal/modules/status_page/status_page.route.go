package status_page

import (
	"peekaping/internal/httpx"
	"peekaping/internal/modules/middleware"
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
	sp.Handle("GET", "/slug/{slug}", r.controller.FindBySlug)
	sp.Handle("GET", "/domain/{domain}", r.controller.FindByDomain)
	sp.Handle("GET", "/slug/{slug}/monitors", r.controller.GetMonitorsBySlug)
	sp.Handle("GET", "/slug/{slug}/monitors/homepage", r.controller.GetMonitorsBySlugForHomepage)

	sp.Use(r.middleware.AllAuth())
	{
		sp.Handle("POST", "", r.controller.Create)
		sp.Handle("GET", "", r.controller.FindAll)
		sp.Handle("GET", "/{id}", r.controller.FindByID)
		sp.Handle("PATCH", "/{id}", r.controller.Update)
		sp.Handle("DELETE", "/{id}", r.controller.Delete)
	}
}
