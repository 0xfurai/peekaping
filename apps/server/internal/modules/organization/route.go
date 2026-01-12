package organization

import (
	"vigi/internal/modules/middleware"

	"github.com/gin-gonic/gin"
)

type OrganizationRoute struct {
	controller    *OrganizationController
	middleware    *middleware.AuthChain
	orgMiddleware *Middleware
}

func NewOrganizationRoute(
	controller *OrganizationController,
	middleware *middleware.AuthChain,
	orgMiddleware *Middleware,
) *OrganizationRoute {
	return &OrganizationRoute{
		controller:    controller,
		middleware:    middleware,
		orgMiddleware: orgMiddleware,
	}
}

func (r *OrganizationRoute) ConnectRoute(
	rg *gin.RouterGroup,
) {
	router := rg.Group("organizations")
	router.Use(r.middleware.AllAuth())

	router.POST("", r.controller.Create)
	router.GET("slug/:slug", r.controller.FindBySlug)
	router.GET(":id", r.controller.FindByID)
	router.PATCH(":id", r.controller.Update)
	router.POST(":id/members", r.controller.AddMember)
	router.GET(":id/members", r.controller.FindMembers)

	// User-centric routes
	userRouter := rg.Group("user/organizations")
	userRouter.Use(r.middleware.AllAuth())
	userRouter.GET("", r.controller.FindUserOrganizations)

	// Invitations routes
	// Public route for viewing invitation
	rg.GET("invitations/:token", r.controller.GetInvitation)

	// Authenticated routes for invitations
	invRouter := rg.Group("invitations")
	invRouter.Use(r.middleware.AllAuth())
	invRouter.POST(":token/accept", r.controller.AcceptInvitation)

	// User invitations route
	userInvRouter := rg.Group("user/invitations")
	userInvRouter.Use(r.middleware.AllAuth())
	userInvRouter.GET("", r.controller.GetUserInvitations)
}
