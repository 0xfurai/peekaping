package internal

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"peekaping/internal/config"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/api_key"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/badge"
	"peekaping/internal/modules/error_reporting"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/metrics"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/tag"
	"peekaping/internal/modules/telemetry"
	"peekaping/internal/modules/websocket"
	"peekaping/internal/probeproto"
	"peekaping/internal/version"

	"go.uber.org/zap"
)

type SystemInput = httpx.Input[httpx.NoBody, struct{}]
type VersionResponse struct {
	Version string `json:"version"`
}

func versionHandler(ctx context.Context, input *SystemInput) (*httpx.Output[VersionResponse], error) {
	return httpx.Reply(http.StatusOK, VersionResponse{Version: version.Version})
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
func newHealthHandler(store stale.Store, logger *zap.SugaredLogger) func(context.Context, *SystemInput) (*httpx.Output[HealthResponse], error) {
	return func(ctx context.Context, input *SystemInput) (*httpx.Output[HealthResponse], error) {
		reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
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
		return httpx.Reply(http.StatusOK, resp)
	}
}

type Server struct {
	Router *httpx.Router
	Cfg    *config.Config
}

// maxRequestBody is the most a request to the API may send, 1 MiB: no route takes more. A route
// with a smaller limit of its own keeps it.
const maxRequestBody = 1 << 20

// ProvideTrustedProxies parses TRUSTED_PROXIES, which stops startup when an entry is invalid.
func ProvideTrustedProxies(cfg *config.Config) (*httpx.TrustedProxies, error) {
	return httpx.ParseTrustedProxies(cfg.TrustedProxies)
}

// ProvideCORSOrigins parses CORS_ORIGINS, which stops startup when an entry is invalid.
func ProvideCORSOrigins(cfg *config.Config) (*httpx.CORSOrigins, error) {
	return httpx.ParseCORSOrigins(cfg.CORSOrigins, cfg.ClientURL)
}

func ProvideServer(
	logger *zap.SugaredLogger,
	cfg *config.Config,
	trustedProxies *httpx.TrustedProxies,
	corsOrigins *httpx.CORSOrigins,
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
	ingesterService ingester.Service,
	maintenanceRoute *maintenance.Route,
	maintenanceController *maintenance.Controller,
	maintenanceService maintenance.Service,
	statusPageRoute *status_page.Route,
	statusPageController *status_page.Controller,
	tagRoute *tag.Route,
	tagController *tag.Controller,
	telemetryRoute *telemetry.Route,
	telemetryController *telemetry.Controller,
	errorReportingRoute *error_reporting.Route,
	errorReportingController *error_reporting.Controller,
	badgeRoute *badge.Route,
	badgeController *badge.Controller,
	apiKeyRoute *api_key.Route,
	apiKeyController *api_key.Controller,
	probeRoute *probe.Route,
	probeController *probe.Controller,
	probeProtocolRoute *protocol.Route,
	probeProtocolController *protocol.Controller,
	staleStore stale.Store,
	metricsCollector *metrics.Collector,
) *Server {
	server := httpx.NewRouter(httpx.RequestID, httpx.Logger(logger, trustedProxies), httpx.Recovery(logger), httpx.CORS(corsOrigins), httpx.LimitBody(maxRequestBody))

	healthHandler := newHealthHandler(staleStore, logger)
	httpx.Register(server, "GET", "/health", huma.Operation{Summary: "Get server health", Tags: []string{"System"}}, healthHandler)

	// Prometheus metrics endpoint (unauthenticated, like /health and /version)
	if cfg.MetricsEnabled {
		server.Handle("GET", "/metrics", metricsCollector.Handler())
	}

	router := server.Group("/api/v1")
	httpx.Register(router, "GET", "/health", huma.Operation{Summary: "Get server health", Tags: []string{"System"}}, healthHandler)
	httpx.Register(router, "GET", "/version", huma.Operation{Summary: "Get server version", Tags: []string{"System"}}, versionHandler)

	// Connect routes
	monitorRoute.ConnectRoute(router, monitorController)
	authRoute.ConnectRoute(router, authController)
	notificationChannelRoute.ConnectRoute(router, notificationChannelController)
	proxyRoute.ConnectRoute(router, proxyController)
	settingRoute.ConnectRoute(router, settingController)
	maintenanceRoute.ConnectRoute(router, maintenanceController)
	statusPageRoute.ConnectRoute(router, statusPageController)
	tagRoute.ConnectRoute(router, tagController)
	telemetryRoute.ConnectRoute(router, telemetryController)
	errorReportingRoute.ConnectRoute(router, errorReportingController)
	badgeRoute.ConnectRoute(router, badgeController)
	apiKeyRoute.ConnectRoute(router, apiKeyController)
	probeRoute.ConnectRoute(router, probeController)

	// The probe protocol is a sibling of the API, not under /api/v1: the probes speak it with their
	// own token, and the admin's credentials don't open it
	probeProtocolRoute.ConnectRoute(server.Group(probeproto.PathPrefix), probeProtocolController)

	// Register push endpoint
	healthcheck.RegisterPushEndpoint(router, monitorService, heartbeatService, ingesterService, maintenanceService, logger)

	// Compatibility entry points; generated documentation lives at /docs.
	server.Handle("GET", "/swagger/{any...}", swaggerHandler(server.API()))
	server.Handle("GET", "/socket.io/{f...}", wsServer.ServeHTTP)
	server.Handle("POST", "/socket.io/{f...}", wsServer.ServeHTTP)

	return &Server{Router: server, Cfg: cfg}
}
