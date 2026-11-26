package api_key

import (
	"net/http"
	"peekaping/internal/httpx"

	"github.com/danielgtaylor/huma/v2"
)

// RouteAuth is the authentication of the API key management routes: a signed-in user, in the
// workspace the request names. The middleware package provides it, and it can't be imported here.
type RouteAuth interface {
	WorkspaceJWTAuth() httpx.Middleware
}

type Route struct {
	controller *Controller
	middleware RouteAuth
}

func NewRoute(
	controller *Controller,
	middleware RouteAuth,
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

	// All other API key management endpoints require JWT authentication, and an admin or owner of
	// the workspace the request runs in
	apiKeys.Use(r.middleware.WorkspaceJWTAuth())
	apiKeys.DocumentWorkspace()
	{
		httpx.Register(apiKeys, "POST", "", huma.Operation{Summary: "Create API key", Tags: []string{"api_key"}, DefaultStatus: http.StatusCreated, Security: httpx.JWTOnly, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.CreateAPIKey)
		httpx.Register(apiKeys, "GET", "", huma.Operation{Summary: "Get API keys", Tags: []string{"api_key"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusInternalServerError, http.StatusUnauthorized}}, controller.GetAPIKeys)
		httpx.Register(apiKeys, "GET", "{id}", huma.Operation{Summary: "Get API key", Tags: []string{"api_key"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusUnauthorized}}, controller.GetAPIKey)
		httpx.Register(apiKeys, "PUT", "{id}", huma.Operation{Summary: "Update API key", Tags: []string{"api_key"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusBadRequest, http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.UpdateAPIKey)
		httpx.Register(apiKeys, "DELETE", "{id}", huma.Operation{Summary: "Delete API key", Tags: []string{"api_key"}, DefaultStatus: http.StatusNoContent, Security: httpx.JWTOnly, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.DeleteAPIKey)
	}
}
