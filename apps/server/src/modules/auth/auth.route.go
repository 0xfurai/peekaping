package auth

import (
	"peekaping/src/modules/bruteforce"

	"github.com/gin-gonic/gin"
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

func (r *Route) ConnectRoute(router *gin.RouterGroup, controller *Controller) {
	auth := router.Group("/auth")
	auth.POST("/register", controller.Register)

	auth.POST("/login", r.bruteforceGuard.Middleware(), controller.Login)

	auth.POST("/refresh", controller.RefreshToken)

	auth.Use(r.middleware.Auth())
	auth.POST("/2fa/setup", controller.SetupTwoFA)
	auth.POST("/2fa/verify", controller.VerifyTwoFA)
	auth.POST("/2fa/disable", controller.DisableTwoFA)
	auth.PUT("/password", controller.UpdatePassword)
}
