package proxy

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

func (uc *Route) ConnectRoute(
	rg *gin.RouterGroup,
	controller *Controller,
) {
	router := rg.Group("proxies")

	router.Use(uc.middleware.AllAuth())
	router.Use(uc.orgMiddleware.RequireOrganization())
	router.GET("", uc.controller.FindAll)
	router.POST("", uc.controller.Create)
	router.GET(":id", uc.controller.FindByID)
	router.PUT(":id", uc.controller.UpdateFull)
	router.PATCH(":id", uc.controller.UpdatePartial)
	router.DELETE(":id", uc.controller.Delete)
}
