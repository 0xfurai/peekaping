package incident

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
	// Public routes (for status page display)
	incidents := rg.Group("incidents")
	incidents.GET("/status-page/:statusPageId", r.controller.FindByStatusPageID)

	// Protected routes (for management)
	incidents.Use(r.middleware.Auth())
	{
		incidents.POST("", r.controller.Create)
		incidents.GET("", r.controller.FindAll)
		incidents.GET("/:id", r.controller.FindByID)
		incidents.PATCH("/:id", r.controller.Update)
		incidents.DELETE("/:id", r.controller.Delete)
	}
}
