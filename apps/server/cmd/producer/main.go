package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"vigi/internal"
	"vigi/internal/config"
	"vigi/internal/infra"
	"vigi/internal/modules/certificate"
	"vigi/internal/modules/events"
	"vigi/internal/modules/healthcheck"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/maintenance"
	"vigi/internal/modules/monitor"
	"vigi/internal/modules/monitor_maintenance"
	"vigi/internal/modules/monitor_notification"
	"vigi/internal/modules/monitor_tag"
	"vigi/internal/modules/monitor_tls_info"
	"vigi/internal/modules/notification_sent_history"
	"vigi/internal/modules/producer"
	"vigi/internal/modules/proxy"
	"vigi/internal/modules/setting"
	"vigi/internal/modules/stats"
	"vigi/internal/modules/tag"
	"vigi/internal/version"
	"syscall"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func main() {
	log.Printf("Starting Vigi Producer v%s", version.Version)

	cfg, err := LoadAndValidate("../..")
	if err != nil {
		log.Fatalf("Failed to load and validate Producer config: %v", err)
	}

	os.Setenv("TZ", cfg.Timezone)

	container := dig.New()

	internalCfg := cfg.ToInternalConfig()

	container.Provide(func() *config.Config { return internalCfg })

	container.Provide(internal.ProvideLogger)

	switch internalCfg.DBType {
	case "postgres", "postgresql", "mysql", "sqlite":
		container.Provide(infra.ProvideSQLDB)
	case "mongo", "mongodb":
		container.Provide(infra.ProvideMongoDB)
	default:
		log.Fatalf("Unsupported DB_TYPE: %s", internalCfg.DBType)
	}

	// Provide Redis infrastructure
	container.Provide(infra.ProvideRedisClient)
	container.Provide(infra.ProvideRedisEventBus)

	// Provide queue infrastructure
	container.Provide(infra.ProvideAsynqClient)
	container.Provide(infra.ProvideAsynqInspector)
	container.Provide(infra.ProvideQueueService)

	// Register module dependencies that producer needs
	heartbeat.RegisterDependencies(container, internalCfg)
	healthcheck.RegisterDependencies(container) // Provides ExecutorRegistry
	tag.RegisterDependencies(container, internalCfg)
	monitor_tag.RegisterDependencies(container, internalCfg)
	monitor.RegisterDependencies(container, internalCfg)
	proxy.RegisterDependencies(container, internalCfg)
	maintenance.RegisterDependencies(container, internalCfg)
	monitor_maintenance.RegisterDependencies(container, internalCfg)
	monitor_notification.RegisterDependencies(container, internalCfg)
	setting.RegisterDependencies(container, internalCfg)
	notification_sent_history.RegisterDependencies(container, internalCfg)
	monitor_tls_info.RegisterDependencies(container, internalCfg)
	certificate.RegisterDependencies(container)
	stats.RegisterDependencies(container, internalCfg)

	// Register producer dependencies
	producer.RegisterDependencies(container)

	// Start the producer
	err = container.Invoke(func(
		prod *producer.Producer,
		eventListener *producer.EventListener,
		eventBus events.EventBus,
		logger *zap.SugaredLogger,
	) error {
		eventListener.Subscribe(eventBus)
		logger.Info("Event listener subscribed to monitor events")

		// Start the producer
		if err := prod.Start(); err != nil {
			return fmt.Errorf("failed to start producer: %w", err)
		}

		logger.Info("Producer started successfully")

		// Wait for termination signal
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		logger.Info("Producer is running. Press Ctrl+C to stop.")
		<-sigChan

		logger.Info("Shutdown signal received, stopping producer...")
		prod.Stop()

		// Close event bus
		if err := eventBus.Close(); err != nil {
			logger.Errorw("Failed to close event bus", "error", err)
		}

		// Perform graceful database shutdown
		if err := infra.GracefulDatabaseShutdown(container, internalCfg, logger); err != nil {
			logger.Errorw("Failed to shutdown database", "error", err)
		}

		logger.Info("Producer stopped gracefully")

		return nil
	})

	if err != nil {
		log.Fatalf("Producer error: %v", err)
	}
}
