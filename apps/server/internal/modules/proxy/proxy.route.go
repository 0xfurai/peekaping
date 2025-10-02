package proxy

import (
	"peekaping/internal/modules/auth"

	"github.com/gin-gonic/gin"
)

type Route struct {
	controller *Controller
	middleware *auth.MiddlewareProvider
}

func NewRoute(
	controller *Controller,
	middleware *auth.MiddlewareProvider,
) *Route {
	return &Route{
		controller,
		middleware,
	}
}

func (uc *Route) ConnectRoute(
	rg *gin.RouterGroup,
	controller *Controller,
) {
	router := rg.Group("proxies")

	router.Use(uc.middleware.AuthWithWorkspace())
	router.GET("", uc.controller.FindAll)
	router.POST("", uc.controller.Create)
	router.GET(":id", uc.controller.FindByID)
	router.PUT(":id", uc.controller.UpdateFull)
	router.PATCH(":id", uc.controller.UpdatePartial)
	router.DELETE(":id", uc.controller.Delete)
}
