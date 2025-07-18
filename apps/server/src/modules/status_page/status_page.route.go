package status_page

import (
	"peekaping/src/modules/auth"

	"github.com/gin-gonic/gin"
)

type Route struct {
	controller *Controller
	middleware *auth.MiddlewareProvider
}

func NewRoute(controller *Controller, middleware *auth.MiddlewareProvider) *Route {
	return &Route{
		controller: controller,
		middleware: middleware,
	}
}

func (r *Route) ConnectRoute(rg *gin.RouterGroup, controller *Controller) {
	// Public routes
	sp := rg.Group("status-pages")
	sp.GET("/slug/:slug", r.controller.FindBySlug)
	sp.GET("/domain/:domain", r.controller.FindByDomain)
	sp.GET("/slug/:slug/monitors", r.controller.GetMonitorsBySlug)
	sp.GET("/slug/:slug/monitors/homepage", r.controller.GetMonitorsBySlugForHomepage)

	sp.Use(r.middleware.Auth())
	{
		sp.POST("", r.controller.Create)
		sp.GET("", r.controller.FindAll)
		sp.GET("/:id", r.controller.FindByID)
		sp.PATCH("/:id", r.controller.Update)
		sp.DELETE("/:id", r.controller.Delete)
	}
}
