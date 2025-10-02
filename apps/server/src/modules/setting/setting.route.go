package setting

import (
	"peekaping/src/modules/auth"

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
		controller, middleware,
	}
}

func (uc *Route) ConnectRoute(
	rg *gin.RouterGroup,
	controller *Controller,
) {
	router := rg.Group("/settings")

	router.Use(uc.middleware.AuthWithWorkspace())

	router.GET("key/:key", uc.controller.GetByKey)
	router.PUT("key/:key", uc.controller.SetByKey)
	router.DELETE("key/:key", uc.controller.DeleteByKey)
}
