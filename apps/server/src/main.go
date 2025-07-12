package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"peekaping/docs"
	"peekaping/src/config"
	"peekaping/src/modules/auth"
	"peekaping/src/modules/cleanup"
	"peekaping/src/modules/events"
	"peekaping/src/modules/healthcheck"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/maintenance"
	"peekaping/src/modules/monitor"
	"peekaping/src/modules/monitor_maintenance"
	"peekaping/src/modules/monitor_notification"
	"peekaping/src/modules/monitor_status_page"
	"peekaping/src/modules/monitor_tag"
	"peekaping/src/modules/notification_channel"
	"peekaping/src/modules/proxy"
	"peekaping/src/modules/setting"
	"peekaping/src/modules/stats"
	"peekaping/src/modules/status_page"
	"peekaping/src/modules/tag"
	"peekaping/src/modules/websocket"
	"peekaping/src/utils"
	"peekaping/src/version"

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
	container.Provide(ProvideLogger)
	container.Provide(ProvideServer)
	container.Provide(websocket.NewServer)

	// database-specific deps
	switch cfg.DBType {
	case "postgres", "postgresql", "mysql", "sqlite":
		container.Provide(ProvideSQLDB)
	case "mongo", "mongodb":
		container.Provide(ProvideMongoDB)
	default:
		panic(fmt.Errorf("unsupported DB_DRIVER %q", cfg.DBType))
	}

	// Register dependencies in the correct order to handle circular dependencies
	events.RegisterDependencies(container)
	heartbeat.RegisterDependencies(container, &cfg)
	monitor.RegisterDependencies(container, &cfg)
	healthcheck.RegisterDependencies(container)
	auth.RegisterDependencies(container, &cfg)
	notification_channel.RegisterDependencies(container, &cfg)
	monitor_notification.RegisterDependencies(container, &cfg)
	proxy.RegisterDependencies(container, &cfg)
	setting.RegisterDependencies(container, &cfg)
	stats.RegisterDependencies(container, &cfg)
	monitor_maintenance.RegisterDependencies(container, &cfg)
	maintenance.RegisterDependencies(container, &cfg)
	status_page.RegisterDependencies(container, &cfg)
	monitor_status_page.RegisterDependencies(container, &cfg)
	tag.RegisterDependencies(container, &cfg)
	monitor_tag.RegisterDependencies(container, &cfg)

	// Start the event healthcheck listener
	err = container.Invoke(func(listener *healthcheck.EventListener, eventBus *events.EventBus) {
		listener.Start(eventBus)
	})

	if err != nil {
		log.Fatal(err)
	}

	// Start cleanup cron job(s)
	err = container.Invoke(func(heartbeatService heartbeat.Service, settingService setting.Service, logger *zap.SugaredLogger) {
		cleanup.StartCleanupCron(heartbeatService, settingService, logger)
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start the health check supervisor
	err = container.Invoke(func(supervisor *healthcheck.HealthCheckSupervisor) {
		if err := supervisor.StartAll(context.Background()); err != nil {
			log.Fatal(err)
		}
	})
	if err != nil {
		log.Fatal(err)
	}

	err = container.Invoke(func(listener *notification_channel.NotificationEventListener, eventBus *events.EventBus) {
		listener.Subscribe(eventBus)
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start the monitor event listener
	err = container.Invoke(func(listener *monitor.MonitorEventListener, eventBus *events.EventBus) {
		listener.Subscribe(eventBus)
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start the server
	err = container.Invoke(func(server *Server) {
		docs.SwaggerInfo.Host = "localhost:" + server.cfg.Port

		port := server.cfg.Port
		if port == "" {
			port = "8084"
		}
		if port[0] != ':' {
			port = ":" + port
		}
		if err := server.router.Run(port); err != nil {
			log.Fatal(err)
		}
	})

	if err != nil {
		log.Fatal(err)
	}
}
