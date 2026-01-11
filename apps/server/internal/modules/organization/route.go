package organization

import (
	"vigi/internal/modules/middleware"

	"github.com/gin-gonic/gin"
)

type OrganizationRoute struct {
	controller *OrganizationController
	middleware *middleware.AuthChain
}

func NewOrganizationRoute(
	controller *OrganizationController,
	middleware *middleware.AuthChain,
) *OrganizationRoute {
	return &OrganizationRoute{
		controller: controller,
		middleware: middleware,
	}
}

func (r *OrganizationRoute) ConnectRoute(
	rg *gin.RouterGroup,
) {
	router := rg.Group("organizations")
	router.Use(r.middleware.AllAuth())

	router.POST("", r.controller.Create)
	router.GET(":id", r.controller.FindByID)
	router.POST(":id/members", r.controller.AddMember)
	router.GET(":id/members", r.controller.FindMembers)

	// User-centric routes
	userRouter := rg.Group("user/organizations")
	userRouter.Use(r.middleware.AllAuth())
	userRouter.GET("", r.controller.FindUserOrganizations)
}
