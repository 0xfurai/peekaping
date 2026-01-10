package maintenance

import (
	"vigi/internal/modules/middleware"

	"github.com/gin-gonic/gin"
)

type Route struct {
	controller *Controller
	middleware *middleware.AuthChain
}

func NewRoute(
	controller *Controller,
	middleware *middleware.AuthChain,
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
	router := rg.Group("maintenances")

	router.Use(uc.middleware.AllAuth())
	router.GET("", uc.controller.FindAll)
	router.POST("", uc.controller.Create)
	router.GET(":id", uc.controller.FindByID)
	router.PUT(":id", uc.controller.UpdateFull)
	router.PATCH(":id", uc.controller.UpdatePartial)
	router.DELETE(":id", uc.controller.Delete)

	router.PATCH(":id/pause", uc.controller.Pause)
	router.PATCH(":id/resume", uc.controller.Resume)
}
