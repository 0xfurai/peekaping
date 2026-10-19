package auth

import (
	"peekaping/internal/httpx"
	"peekaping/internal/modules/bruteforce"
)

type Route struct {
	controller      *Controller
	middleware      *MiddlewareProvider
	bruteforceGuard *bruteforce.Guard
}

func NewRoute(
	controller *Controller,
	middleware *MiddlewareProvider,
	bruteforceGuard *bruteforce.Guard,
) *Route {
	return &Route{
		controller,
		middleware,
		bruteforceGuard,
	}
}

func (r *Route) ConnectRoute(router *httpx.Router, controller *Controller) {
	auth := router.Group("/auth")
	auth.Handle("POST", "/register", controller.Register)

	auth.Handle("POST", "/login", controller.Login, r.bruteforceGuard.Middleware())

	auth.Handle("POST", "/refresh", controller.RefreshToken)

	auth.Use(r.middleware.Auth())
	auth.Handle("POST", "/2fa/setup", controller.SetupTwoFA)
	auth.Handle("POST", "/2fa/verify", controller.VerifyTwoFA)
	auth.Handle("POST", "/2fa/disable", controller.DisableTwoFA)
	auth.Handle("PUT", "/password", controller.UpdatePassword)
}
