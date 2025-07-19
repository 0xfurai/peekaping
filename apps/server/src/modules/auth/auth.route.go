package auth

import (
	"github.com/gin-gonic/gin"
)

type Route struct {
	controller *Controller
	middleware *MiddlewareProvider
}

func NewRoute(
	controller *Controller,
	middleware *MiddlewareProvider,
) *Route {
	return &Route{
		controller,
		middleware,
	}
}

func (r *Route) ConnectRoute(router *gin.RouterGroup, controller *Controller) {
	auth := router.Group("/auth")

	// Apply bruteforce protection middleware to all auth routes
	auth.Use(r.middleware.BruteforceProtection())

	auth.POST("/register", controller.Register)
	auth.POST("/login", controller.Login)
	auth.POST("/refresh", controller.RefreshToken)

	auth.Use(r.middleware.Auth())
	auth.POST("/2fa/setup", controller.SetupTwoFA)
	auth.POST("/2fa/verify", controller.VerifyTwoFA)
	auth.POST("/2fa/disable", controller.DisableTwoFA)
	auth.PUT("/password", controller.UpdatePassword)
}
