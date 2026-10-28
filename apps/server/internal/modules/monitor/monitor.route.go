package monitor

import (
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/middleware"

	"github.com/danielgtaylor/huma/v2"
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

	httpx.Register(router, "GET", "", huma.Operation{Summary: "Get monitors", Tags: []string{"monitor"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.monitorController.FindAll)
	httpx.Register(router, "GET", "batch", huma.Operation{Summary: "Get monitors by IDs", Tags: []string{"monitor"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.monitorController.FindByIDs)
	httpx.Register(router, "POST", "", huma.Operation{Summary: "Create monitor", Responses: map[string]*huma.Response{"400": httpx.ErrorVariants[PushTokenErrorResponse](router, 400)}, Tags: []string{"monitor"}, DefaultStatus: http.StatusCreated, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.monitorController.Create)
	httpx.Register(router, "GET", "{id}", huma.Operation{Summary: "Get monitor by ID", Tags: []string{"monitor"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusUnauthorized}}, uc.monitorController.FindByID)
	httpx.Register(router, "PUT", "{id}", huma.Operation{Summary: "Update monitor", Responses: map[string]*huma.Response{"400": httpx.ErrorVariants[PushTokenErrorResponse](router, 400)}, Tags: []string{"monitor"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.monitorController.UpdateFull)
	httpx.Register(router, "PATCH", "{id}", huma.Operation{Summary: "Update monitor", Responses: map[string]*huma.Response{"400": httpx.ErrorVariants[PushTokenErrorResponse](router, 400)}, Tags: []string{"monitor"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.monitorController.UpdatePartial)
	httpx.Register(router, "DELETE", "{id}", huma.Operation{Summary: "Delete monitor", Tags: []string{"monitor"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.monitorController.Delete)
	httpx.Register(router, "POST", "{id}/reset", huma.Operation{Summary: "Reset monitor data (heartbeats and stats)", Tags: []string{"monitor"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.monitorController.ResetMonitorData)
	httpx.Register(router, "GET", "{id}/heartbeats", huma.Operation{Summary: "Get paginated heartbeats for a monitor", Tags: []string{"monitor"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.monitorController.FindByMonitorIDPaginated)
	httpx.Register(router, "GET", "{id}/stats/uptime", huma.Operation{Summary: "Get monitor uptime stats (24h, 30d, 365d)", Tags: []string{"monitor"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.monitorController.GetUptimeStats)
	httpx.Register(router, "GET", "{id}/stats/points", huma.Operation{Summary: "Get monitor stat points (ping/up/down) from stats tables", Tags: []string{"monitor"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.monitorController.GetStatPoints)
	httpx.Register(router, "GET", "{id}/tls", huma.Operation{Summary: "Get monitor TLS certificate information", Tags: []string{"monitor"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.monitorController.GetTLSInfo)
}
