package tag

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

func NewRoute(
	controller *Controller,
	middleware *middleware.AuthChain,
	orgMiddleware *organization.Middleware,
) *Route {
	return &Route{
		controller,
		middleware,
		orgMiddleware,
	}
}

func (r *Route) ConnectRoute(
	rg *gin.RouterGroup,
	controller *Controller,
) {
	router := rg.Group("tags")

	router.Use(r.middleware.AllAuth())
	router.Use(r.orgMiddleware.RequireOrganization())

	router.GET("", controller.FindAll)
	router.POST("", controller.Create)
	router.GET("/:id", controller.FindByID)
	router.PUT("/:id", controller.UpdateFull)
	router.PATCH("/:id", controller.UpdatePartial)
	router.DELETE("/:id", controller.Delete)
}
