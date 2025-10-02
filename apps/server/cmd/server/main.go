package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"peekaping/docs"
	"peekaping/internal"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/badge"
	"peekaping/internal/modules/bruteforce"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/cleanup"
	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_maintenance"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/tag"
	"peekaping/internal/modules/websocket"
	"peekaping/internal/utils"
	"peekaping/internal/version"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

// @title			Peekaping API
// @BasePath	/api/v1
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	docs.SwaggerInfo.Version = version.Version

	utils.RegisterCustomValidators()

	cfg, err := config.LoadConfig[config.Config]("../..")

	if err != nil {
		panic(err)
	}

	err = config.ValidateDatabaseCustomRules(config.ExtractDBConfig(&cfg))
	if err != nil {
		panic(err)
	}

	os.Setenv("TZ", cfg.Timezone)

	container := dig.New()

	// Provide dependencies
	container.Provide(func() *config.Config { return &cfg })
	container.Provide(internal.ProvideLogger)
	container.Provide(internal.ProvideServer)
	container.Provide(websocket.NewServer)

	// database-specific deps
	switch cfg.DBType {
	case "postgres", "postgresql", "mysql", "sqlite":
		container.Provide(infra.ProvideSQLDB)
	case "mongo", "mongodb":
		container.Provide(infra.ProvideMongoDB)
	default:
		panic(fmt.Errorf("unsupported DB_DRIVER %q", cfg.DBType))
	}

	// Provide Redis event bus
	container.Provide(infra.ProvideRedisClient)
	container.Provide(infra.ProvideRedisEventBus)

	// Register dependencies in the correct order to handle circular dependencies
	events.RegisterDependencies(container)
	heartbeat.RegisterDependencies(container, &cfg)
	monitor.RegisterDependencies(container, &cfg)
	healthcheck.RegisterDependencies(container)
	bruteforce.RegisterDependencies(container, &cfg)
	auth.RegisterDependencies(container, &cfg)
	notification_channel.RegisterDependencies(container, &cfg)
	monitor_notification.RegisterDependencies(container, &cfg)
	proxy.RegisterDependencies(container, &cfg)
	setting.RegisterDependencies(container, &cfg)
	notification_sent_history.RegisterDependencies(container, &cfg)
	monitor_tls_info.RegisterDependencies(container, &cfg)
	certificate.RegisterDependencies(container)
	stats.RegisterDependencies(container, &cfg)
	monitor_maintenance.RegisterDependencies(container, &cfg)
	maintenance.RegisterDependencies(container, &cfg)
	status_page.RegisterDependencies(container, &cfg)
	monitor_status_page.RegisterDependencies(container, &cfg)
	domain_status_page.RegisterDependencies(container, &cfg)
	tag.RegisterDependencies(container, &cfg)
	monitor_tag.RegisterDependencies(container, &cfg)
	badge.RegisterDependencies(container, &cfg)

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

	// Start the server
	err = container.Invoke(func(server *internal.Server) {
		docs.SwaggerInfo.Host = "localhost:" + server.Cfg.Port

		port := server.Cfg.Port
		if port == "" {
			port = "8084"
		}
		if port[0] != ':' {
			port = ":" + port
		}
		if err := server.Router.Run(port); err != nil {
			log.Fatal(err)
		}
	})

	if err != nil {
		log.Fatal(err)
	}
}
