package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"vigi/docs"
	"vigi/internal"
	"vigi/internal/config"
	"vigi/internal/infra"
	"vigi/internal/modules/api_key"
	"vigi/internal/modules/auth"
	"vigi/internal/modules/badge"
	"vigi/internal/modules/bruteforce"
	"vigi/internal/modules/certificate"
	"vigi/internal/modules/cleanup"
	"vigi/internal/modules/domain_status_page"
	"vigi/internal/modules/events"
	"vigi/internal/modules/healthcheck"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/maintenance"
	"vigi/internal/modules/middleware"
	"vigi/internal/modules/monitor"
	"vigi/internal/modules/monitor_maintenance"
	"vigi/internal/modules/monitor_notification"
	"vigi/internal/modules/monitor_status_page"
	"vigi/internal/modules/monitor_tag"
	"vigi/internal/modules/monitor_tls_info"
	"vigi/internal/modules/notification_channel"
	"vigi/internal/modules/notification_sent_history"
	"vigi/internal/modules/organization"
	"vigi/internal/modules/proxy"
	"vigi/internal/modules/queue"
	"vigi/internal/modules/setting"
	"vigi/internal/modules/stats"
	"vigi/internal/modules/status_page"
	"vigi/internal/modules/tag"
	"vigi/internal/modules/websocket"
	"vigi/internal/utils"
	"vigi/internal/version"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

// @title			Vigi API
// @BasePath	/api/v1
// @securityDefinitions.apikey JwtAuth
// @in header
// @name Authorization
// @description JWT token authentication (Bearer token format)
// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name X-API-Key
// @description API key authentication (pk_ prefix format)
// @securityDefinitions.apikey OrgIdAuth
// @in header
// @name X-Organization-ID
// @description Organization ID authentication (header format)
func main() {
	docs.SwaggerInfo.Version = version.Version

	utils.RegisterCustomValidators()

	// Load and validate API-specific config
	cfg, err := LoadAndValidate("../..")
	if err != nil {
		log.Fatalf("Failed to load and validate API config: %v", err)
	}

	os.Setenv("TZ", cfg.Timezone)

	container := dig.New()

	// Convert to internal config format for dependency injection
	internalCfg := cfg.ToInternalConfig()

	// Provide dependencies
	container.Provide(func() *config.Config { return internalCfg })
	container.Provide(internal.ProvideLogger)
	container.Provide(internal.ProvideServer)
	container.Provide(websocket.NewServer)

	// database-specific deps
	switch internalCfg.DBType {
	case "postgres", "postgresql", "mysql", "sqlite":
		container.Provide(infra.ProvideSQLDB)
	case "mongo", "mongodb":
		container.Provide(infra.ProvideMongoDB)
	default:
		panic(fmt.Errorf("unsupported DB_DRIVER %q", internalCfg.DBType))
	}

	// Provide Redis event bus
	container.Provide(infra.ProvideRedisClient)
	container.Provide(infra.ProvideRedisEventBus)

	// Provide queue infrastructure (for push endpoint)
	container.Provide(infra.ProvideAsynqClient)
	container.Provide(infra.ProvideAsynqInspector)
	container.Provide(infra.ProvideQueueService)

	// Register dependencies in the correct order to handle circular dependencies
	heartbeat.RegisterDependencies(container, internalCfg)
	monitor.RegisterDependencies(container, internalCfg)
	healthcheck.RegisterDependencies(container)
	bruteforce.RegisterDependencies(container, internalCfg)
	auth.RegisterDependencies(container, internalCfg)
	notification_channel.RegisterDependencies(container, internalCfg)
	monitor_notification.RegisterDependencies(container, internalCfg)
	proxy.RegisterDependencies(container, internalCfg)
	setting.RegisterDependencies(container, internalCfg)
	notification_sent_history.RegisterDependencies(container, internalCfg)
	monitor_tls_info.RegisterDependencies(container, internalCfg)
	certificate.RegisterDependencies(container)
	stats.RegisterDependencies(container, internalCfg)
	monitor_maintenance.RegisterDependencies(container, internalCfg)
	maintenance.RegisterDependencies(container, internalCfg)
	status_page.RegisterDependencies(container, internalCfg)
	monitor_status_page.RegisterDependencies(container, internalCfg)
	domain_status_page.RegisterDependencies(container, internalCfg)
	tag.RegisterDependencies(container, internalCfg)
	monitor_tag.RegisterDependencies(container, internalCfg)
	badge.RegisterDependencies(container, internalCfg)
	queue.RegisterDependencies(container, internalCfg)
	api_key.RegisterDependencies(container, internalCfg)
	organization.RegisterDependencies(container, internalCfg)
	middleware.RegisterDependencies(container)

	// Start the event healthcheck listener
	err = container.Invoke(func(listener *healthcheck.EventListener, eventBus events.EventBus) {
		listener.Start(eventBus)
	})

	if err != nil {
		log.Fatal(err)
	}

	// Start cleanup cron job(s)
	err = container.Invoke(func(
		heartbeatService heartbeat.Service,
		settingService setting.Service,
		notificationHistoryService notification_sent_history.Service,
		tlsInfoService monitor_tls_info.Service,
		logger *zap.SugaredLogger,
	) {
		cleanup.StartCleanupCron(heartbeatService, settingService, notificationHistoryService, tlsInfoService, logger)
	})
	if err != nil {
		log.Fatal(err)
	}

	// Initialize JWT settings
	err = container.Invoke(func(settingService setting.Service) {
		if err := settingService.InitializeSettings(context.Background()); err != nil {
			log.Fatalf("Failed to initialize JWT settings: %v", err)
		}
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start the health check supervisor
	// err = container.Invoke(func(supervisor *healthcheck.HealthCheckSupervisor) {
	// 	if err := supervisor.StartAll(context.Background()); err != nil {
	// 		log.Fatal(err)
	// 	}
	// })
	// if err != nil {
	// 	log.Fatal(err)
	// }

	err = container.Invoke(func(listener *notification_channel.NotificationEventListener, eventBus events.EventBus) {
		listener.Subscribe(eventBus)
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start the monitor event listener
	err = container.Invoke(func(listener *monitor.MonitorEventListener, eventBus events.EventBus) {
		listener.Subscribe(eventBus)
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start the server with graceful shutdown
	err = container.Invoke(func(
		server *internal.Server,
		eventBus events.EventBus,
		logger *zap.SugaredLogger,
	) error {
		docs.SwaggerInfo.Host = "localhost:" + server.Cfg.Port

		port := server.Cfg.Port
		if port == "" {
			port = "8084"
		}
		if port[0] != ':' {
			port = ":" + port
		}

		// Set up signal handling for graceful shutdown
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		// Start server in a goroutine
		go func() {
			logger.Infof("Starting server on port %s", port)
			if err := server.Router.Run(port); err != nil {
				logger.Fatalf("Failed to start server: %v", err)
			}
		}()

		// Wait for shutdown signal
		<-sigChan
		logger.Info("Shutdown signal received, starting graceful shutdown...")
		// Close event bus
		if err := eventBus.Close(); err != nil {
			logger.Errorw("Failed to close event bus", "error", err)
		}

		// Perform graceful database shutdown
		if err := infra.GracefulDatabaseShutdown(container, internalCfg, logger); err != nil {
			logger.Errorw("Failed to shutdown database", "error", err)
		}

		logger.Info("Server stopped gracefully")
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}
}
