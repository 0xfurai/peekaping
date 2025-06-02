package monitor

import (
	"peekaping/src/modules/auth"

	"github.com/gin-gonic/gin"
)

type MonitorRoute struct {
	monitorController *MonitorController
	middleware        *auth.MiddlewareProvider
}

func NewMonitorRoute(
	monitorController *MonitorController,
	middleware *auth.MiddlewareProvider,
) *MonitorRoute {
	return &MonitorRoute{
		monitorController,
		middleware,
	}
}

func (uc *MonitorRoute) ConnectRoute(
	rg *gin.RouterGroup,
	monitorController *MonitorController,
) {
	router := rg.Group("monitors")
	// router.Use(uc.middleware.Auth())

	router.GET("", uc.monitorController.FindAll)
	router.POST("", uc.monitorController.Create)
	router.GET(":id", uc.monitorController.FindByID)
	router.PUT(":id", uc.monitorController.UpdateFull)
	router.PATCH(":id", uc.monitorController.UpdatePartial)
	router.DELETE(":id", uc.monitorController.Delete)
	router.GET(":id/chartpoints", uc.monitorController.GetMonitorChartPoints)
	router.GET(":id/heartbeats", uc.monitorController.FindByMonitorIDPaginated)
	router.GET(":id/uptime", uc.monitorController.GetUptimeStats)
}
