package setting

import (
	"vigi/internal/modules/middleware"

	"github.com/gin-gonic/gin"
)

type Route struct {
	controller *Controller
	middleware *middleware.AuthChain
}

func NewRoute(
	controller *Controller,
	middleware *middleware.AuthChain,
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

	router.Use(uc.middleware.AllAuth())

	router.GET("key/:key", uc.controller.GetByKey)
	router.PUT("key/:key", uc.controller.SetByKey)
	router.DELETE("key/:key", uc.controller.DeleteByKey)
}
