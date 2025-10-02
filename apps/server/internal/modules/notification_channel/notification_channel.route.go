package notification_channel

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
	router := rg.Group("notification-channels")

	router.Use(uc.middleware.Auth())

	router.GET("", controller.FindAll)
	router.POST("", controller.Create)
	router.POST("/test", controller.Test)
	router.GET("/:id", controller.FindByID)
	router.PUT("/:id", controller.UpdateFull)
	router.PATCH("/:id", controller.UpdatePartial)
	router.DELETE("/:id", controller.Delete)
}
