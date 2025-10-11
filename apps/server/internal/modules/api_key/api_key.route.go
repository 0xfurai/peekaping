package api_key

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

func (r *Route) ConnectRoute(router *gin.RouterGroup, controller *Controller) {
	apiKeys := router.Group("api-keys")

	// Config endpoint doesn't require authentication
	apiKeys.GET("config", controller.GetAPIKeyConfig)

	// All other API key management endpoints require JWT authentication
	apiKeys.Use(r.middleware.Auth())
	{
		apiKeys.POST("", controller.CreateAPIKey)
		apiKeys.GET("", controller.GetAPIKeys)
		apiKeys.GET(":id", controller.GetAPIKey)
		apiKeys.PUT(":id", controller.UpdateAPIKey)
		apiKeys.DELETE(":id", controller.DeleteAPIKey)
	}
}
