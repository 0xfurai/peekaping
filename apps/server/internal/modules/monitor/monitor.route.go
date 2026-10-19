package monitor

import (
	"peekaping/internal/httpx"
	"peekaping/internal/modules/middleware"
)

type MonitorRoute struct {
	monitorController *MonitorController
	middleware        *middleware.AuthChain
}

func NewMonitorRoute(
	monitorController *MonitorController,
	middleware *middleware.AuthChain,
) *MonitorRoute {
	return &MonitorRoute{
		monitorController,
		middleware,
	}
}

func (uc *MonitorRoute) ConnectRoute(
	rg *httpx.Router,
	monitorController *MonitorController,
) {
	router := rg.Group("monitors")
	router.Use(uc.middleware.AllAuth())

	router.Handle("GET", "", uc.monitorController.FindAll)
	router.Handle("GET", "batch", uc.monitorController.FindByIDs)
	router.Handle("POST", "", uc.monitorController.Create)
	router.Handle("GET", "{id}", uc.monitorController.FindByID)
	router.Handle("PUT", "{id}", uc.monitorController.UpdateFull)
	router.Handle("PATCH", "{id}", uc.monitorController.UpdatePartial)
	router.Handle("DELETE", "{id}", uc.monitorController.Delete)
	router.Handle("POST", "{id}/reset", uc.monitorController.ResetMonitorData)
	router.Handle("GET", "{id}/heartbeats", uc.monitorController.FindByMonitorIDPaginated)
	router.Handle("GET", "{id}/stats/uptime", uc.monitorController.GetUptimeStats)
	router.Handle("GET", "{id}/stats/points", uc.monitorController.GetStatPoints)
	router.Handle("GET", "{id}/tls", uc.monitorController.GetTLSInfo)
}
