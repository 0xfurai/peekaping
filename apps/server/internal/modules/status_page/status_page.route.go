package status_page

import (
	"vigi/internal/modules/middleware"
	"vigi/internal/modules/organization"

	"github.com/gin-gonic/gin"
)

type Route struct {
	controller    *Controller
	middleware    *middleware.AuthChain
	orgMiddleware *organization.Middleware
}

func NewRoute(controller *Controller, middleware *middleware.AuthChain, orgMiddleware *organization.Middleware) *Route {
	return &Route{
		controller:    controller,
		middleware:    middleware,
		orgMiddleware: orgMiddleware,
	}
}

func (r *Route) ConnectRoute(rg *gin.RouterGroup, controller *Controller) {
	// Public routes
	sp := rg.Group("status-pages")
	sp.GET("/slug/:slug", r.controller.FindBySlug)
	sp.GET("/domain/:domain", r.controller.FindByDomain)
	sp.GET("/slug/:slug/monitors", r.controller.GetMonitorsBySlug)
	sp.GET("/slug/:slug/monitors/homepage", r.controller.GetMonitorsBySlugForHomepage)

	sp.Use(r.middleware.AllAuth())
	sp.Use(r.orgMiddleware.RequireOrganization())
	{
		sp.POST("", r.controller.Create)
		sp.GET("", r.controller.FindAll)
		sp.GET("/:id", r.controller.FindByID)
		sp.PATCH("/:id", r.controller.Update)
		sp.DELETE("/:id", r.controller.Delete)
	}
}
