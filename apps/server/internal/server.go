package internal

import (
	"context"
	"net/http"
	"time"

	_ "peekaping/docs"
	"peekaping/internal/config"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/api_key"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/badge"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/tag"
	"peekaping/internal/modules/websocket"
	"peekaping/internal/version"

	"go.uber.org/zap"
)

// @Summary      Get server version
// @Description  Returns the current server version
// @Tags         System
// @Produce      json
// @Success      200  {object}  map[string]string  "{"version": "1.2.3"}"
// @Router       /version [get]
func versionHandler(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"version": version.Version})
}

// HealthResponse is the server health
type HealthResponse struct {
	Status string `json:"status" example:"success"`
	// StaleMonitors is how many active monitors Peekaping has stopped checking; null if Redis can't be read
	StaleMonitors *int64 `json:"stale_monitors" extensions:"x-nullable"`
	// WatchdogRanAt is when the stale-monitor watchdog last ran; null if it never has or Redis can't be read
	WatchdogRanAt *time.Time `json:"watchdog_ran_at" extensions:"x-nullable"`
}

// newHealthHandler answers 200 while the server is up, so the docker healthcheck only
// watches the API. An external watcher alerts on stale_monitors or an old watchdog_ran_at.
//
// @Summary      Get server health
// @Description  Returns the server health and how many monitors Peekaping has stopped checking
// @Tags         System
// @Produce      json
// @Success      200  {object}  HealthResponse
// @Router       /health [get]
func newHealthHandler(store *stale.Store, logger *zap.SugaredLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		resp := HealthResponse{Status: "success"}
		if count, err := store.Count(reqCtx); err != nil {
			logger.Warnw("Failed to read the stale monitor count", "error", err)
		} else {
			resp.StaleMonitors = &count
		}
		if ranAt, err := store.RanAt(reqCtx); err != nil {
			logger.Warnw("Failed to read when the stale-monitor watchdog last ran", "error", err)
		} else {
			resp.WatchdogRanAt = ranAt
		}
		httpx.JSON(w, http.StatusOK, resp)
	}
}

type Server struct {
	Router *httpx.Router
	Cfg    *config.Config
}

func ProvideServer(
	logger *zap.SugaredLogger,
	cfg *config.Config,
	monitorRoute *monitor.MonitorRoute,
	monitorController *monitor.MonitorController,
	authRoute *auth.Route,
	authController *auth.Controller,
	wsServer *websocket.Server,
	notificationChannelRoute *notification_channel.Route,
	notificationChannelController *notification_channel.Controller,
	proxyRoute *proxy.Route,
	proxyController *proxy.Controller,
	settingRoute *setting.Route,
	settingController *setting.Controller,
	heartbeatService heartbeat.Service,
	monitorService monitor.Service,
	queueService queue.Service,
	maintenanceRoute *maintenance.Route,
	maintenanceController *maintenance.Controller,
	statusPageRoute *status_page.Route,
	statusPageController *status_page.Controller,
	tagRoute *tag.Route,
	tagController *tag.Controller,
	badgeRoute *badge.Route,
	badgeController *badge.Controller,
	apiKeyRoute *api_key.Route,
	apiKeyController *api_key.Controller,
	staleStore *stale.Store,
) *Server {
	middleware := []httpx.Middleware{httpx.Recovery(logger), httpx.CORS}
	if cfg.Mode == "dev" {
		middleware = append([]httpx.Middleware{httpx.Logger(logger)}, middleware...)
	}
	server := httpx.NewRouter(middleware...)

	healthHandler := newHealthHandler(staleStore, logger)
	server.Handle("GET", "/health", healthHandler)
	router := server.Group("/api/v1")
	router.Handle("GET", "/health", healthHandler)
	router.Handle("GET", "/version", versionHandler)

	// Connect routes
	monitorRoute.ConnectRoute(router, monitorController)
	authRoute.ConnectRoute(router, authController)
	notificationChannelRoute.ConnectRoute(router, notificationChannelController)
	proxyRoute.ConnectRoute(router, proxyController)
	settingRoute.ConnectRoute(router, settingController)
	maintenanceRoute.ConnectRoute(router, maintenanceController)
	statusPageRoute.ConnectRoute(router, statusPageController)
	tagRoute.ConnectRoute(router, tagController)
	badgeRoute.ConnectRoute(router, badgeController)
	apiKeyRoute.ConnectRoute(router, apiKeyController)

	// Register push endpoint
	healthcheck.RegisterPushEndpoint(router, monitorService, heartbeatService, queueService, logger)

	// Documentation uses the existing generated Swag specification until PP-199.
	server.Handle("GET", "/swagger/{any...}", swaggerHandler())
	server.Handle("GET", "/socket.io/{f...}", wsServer.ServeHTTP)
	server.Handle("POST", "/socket.io/{f...}", wsServer.ServeHTTP)

	return &Server{Router: server, Cfg: cfg}
}
