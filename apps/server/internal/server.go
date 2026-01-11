package internal

import (
	"net/http"
	_ "vigi/docs"
	"vigi/internal/config"
	"vigi/internal/modules/api_key"
	"vigi/internal/modules/auth"
	"vigi/internal/modules/badge"
	"vigi/internal/modules/healthcheck"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/maintenance"
	"vigi/internal/modules/monitor"
	"vigi/internal/modules/notification_channel"
	"vigi/internal/modules/organization"
	"vigi/internal/modules/proxy"
	"vigi/internal/modules/queue"
	"vigi/internal/modules/setting"
	"vigi/internal/modules/status_page"
	"vigi/internal/modules/tag"
	"vigi/internal/modules/websocket"
	"vigi/internal/version"

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

// @Summary      Get server health
// @Description  Returns the current server health
// @Tags         System
// @Produce      json
// @Success      200  {object}  map[string]string  "{"status": "success"}"
// @Router       /health [get]
func healthHandler(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{"status": "success"})
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
	organizationRoute *organization.OrganizationRoute,
	organizationController *organization.OrganizationController,
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
	organizationRoute.ConnectRoute(router)

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
