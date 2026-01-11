package monitor

import (
	"vigi/internal/modules/middleware"
	"vigi/internal/modules/organization"

	"github.com/gin-gonic/gin"
)

type MonitorRoute struct {
	monitorController *MonitorController
	middleware        *middleware.AuthChain
	orgMiddleware     *organization.Middleware
}

func NewMonitorRoute(
	monitorController *MonitorController,
	middleware *middleware.AuthChain,
	orgMiddleware *organization.Middleware,
) *MonitorRoute {
	return &MonitorRoute{
		monitorController: monitorController,
		middleware:        middleware,
		orgMiddleware:     orgMiddleware,
	}
}

func (uc *MonitorRoute) ConnectRoute(
	rg *gin.RouterGroup,
	monitorController *MonitorController,
) {
	router := rg.Group("monitors")
	router.Use(uc.middleware.AllAuth())
	router.Use(uc.orgMiddleware.RequireOrganization())

	router.GET("", uc.monitorController.FindAll)
	router.GET("batch", uc.monitorController.FindByIDs)
	router.POST("", uc.monitorController.Create)
	router.GET(":id", uc.monitorController.FindByID)
	router.PUT(":id", uc.monitorController.UpdateFull)
	router.PATCH(":id", uc.monitorController.UpdatePartial)
	router.DELETE(":id", uc.monitorController.Delete)
	router.POST(":id/reset", uc.monitorController.ResetMonitorData)
	router.GET(":id/heartbeats", uc.monitorController.FindByMonitorIDPaginated)
	router.GET(":id/stats/uptime", uc.monitorController.GetUptimeStats)
	router.GET(":id/stats/points", uc.monitorController.GetStatPoints)
	router.GET(":id/tls", uc.monitorController.GetTLSInfo)
}
