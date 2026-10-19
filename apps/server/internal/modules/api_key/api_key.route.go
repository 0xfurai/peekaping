package api_key

import (
	"peekaping/internal/httpx"
	"peekaping/internal/modules/auth"
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

func (r *Route) ConnectRoute(router *httpx.Router, controller *Controller) {
	apiKeys := router.Group("api-keys")

	// Config endpoint doesn't require authentication
	apiKeys.Handle("GET", "config", controller.GetAPIKeyConfig)

	// All other API key management endpoints require JWT authentication
	apiKeys.Use(r.middleware.Auth())
	{
		apiKeys.Handle("POST", "", controller.CreateAPIKey)
		apiKeys.Handle("GET", "", controller.GetAPIKeys)
		apiKeys.Handle("GET", "{id}", controller.GetAPIKey)
		apiKeys.Handle("PUT", "{id}", controller.UpdateAPIKey)
		apiKeys.Handle("DELETE", "{id}", controller.DeleteAPIKey)
	}
}
