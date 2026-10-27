package api_key

import (
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/auth"

	"github.com/danielgtaylor/huma/v2"
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
	httpx.Register(apiKeys, "GET", "config", huma.Operation{Summary: "Get API key configuration", Tags: []string{"api_key"}, DefaultStatus: http.StatusOK, Errors: []int{}}, controller.GetAPIKeyConfig)

	// All other API key management endpoints require JWT authentication
	apiKeys.Use(r.middleware.Auth())
	{
		httpx.Register(apiKeys, "POST", "", huma.Operation{Summary: "Create API key", Tags: []string{"api_key"}, DefaultStatus: http.StatusCreated, Security: httpx.JWTOnly, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.CreateAPIKey)
		httpx.Register(apiKeys, "GET", "", huma.Operation{Summary: "Get API keys", Tags: []string{"api_key"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusInternalServerError, http.StatusUnauthorized}}, controller.GetAPIKeys)
		httpx.Register(apiKeys, "GET", "{id}", huma.Operation{Summary: "Get API key", Tags: []string{"api_key"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusUnauthorized}}, controller.GetAPIKey)
		httpx.Register(apiKeys, "PUT", "{id}", huma.Operation{Summary: "Update API key", Tags: []string{"api_key"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusBadRequest, http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.UpdateAPIKey)
		httpx.Register(apiKeys, "DELETE", "{id}", huma.Operation{Summary: "Delete API key", Tags: []string{"api_key"}, DefaultStatus: http.StatusNoContent, Security: httpx.JWTOnly, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.DeleteAPIKey)
	}
}
