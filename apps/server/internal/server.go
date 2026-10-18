package internal

import (
	"context"
	"net/http"
	"time"

	_ "peekaping/docs"
	"peekaping/internal/config"
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

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"
)

// @Summary      Get server version
// @Description  Returns the current server version
// @Tags         System
// @Produce      json
// @Success      200  {object}  map[string]string  "{"version": "1.2.3"}"
// @Router       /version [get]
func versionHandler(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{"version": version.Version})
}

// HealthResponse is the server health
type HealthResponse struct {
	Status string `json:"status" example:"success"`
	// StaleMonitors is how many active monitors Peekaping has stopped checking; null if Redis can't be read
	StaleMonitors *int64 `json:"stale_monitors"`
	// WatchdogRanAt is when the stale-monitor watchdog last ran; null if it never has
	WatchdogRanAt *time.Time `json:"watchdog_ran_at"`
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
func newHealthHandler(store *stale.Store, logger *zap.SugaredLogger) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		reqCtx, cancel := context.WithTimeout(ctx.Request.Context(), 2*time.Second)
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
		ctx.JSON(http.StatusOK, resp)
	}
}

type Server struct {
	Router *gin.Engine
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
	// Initialize server based on mode
	var server *gin.Engine
	if cfg.Mode == "dev" {
		// Development: use default with logger and recovery middleware
		server = gin.Default()
	} else {
		// Production/Test: use clean instance with only recovery middleware
		gin.SetMode(gin.ReleaseMode)
		server = gin.New()
		server.Use(gin.Recovery()) // Always use recovery middleware to prevent crashes
	}

	server.RedirectTrailingSlash = false

	// CORS configuration
	server.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Origin", "X-Requested-With", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Authorization"},
		AllowCredentials: true,
	}))

	// server.Use(LogMiddleware(logger))

	healthHandler := newHealthHandler(staleStore, logger)
	server.GET("/health", healthHandler)
	router := server.Group("/api/v1")
	router.GET("/health", healthHandler)
	router.GET("/version", versionHandler)

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

	// Swagger routes
	url := ginSwagger.URL("/swagger/doc.json")
	server.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, url))

	// WebSocket route
	server.GET("/socket.io/*f", func(c *gin.Context) {
		wsServer.ServeHTTP(c.Writer, c.Request)
	})
	server.POST("/socket.io/*f", func(c *gin.Context) {
		wsServer.ServeHTTP(c.Writer, c.Request)
	})

	return &Server{Router: server, Cfg: cfg}
}
