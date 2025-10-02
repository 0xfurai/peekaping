package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_maintenance"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/worker"
	"peekaping/internal/version"
	"syscall"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func main() {
	log.Printf("Starting Peekaping Worker v%s", version.Version)

	// Load configuration
	cfg, err := config.LoadConfig[config.Config]("../..")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Validate database configuration
	err = config.ValidateDatabaseCustomRules(config.ExtractDBConfig(&cfg))
	if err != nil {
		log.Fatalf("Failed to validate database config: %v", err)
	}

	// Set timezone
	os.Setenv("TZ", cfg.Timezone)

	// Create DI container
	container := dig.New()

	// Provide configuration
	container.Provide(func() *config.Config { return &cfg })

	// Provide logger
	container.Provide(func(cfg *config.Config) (*zap.SugaredLogger, error) {
		var zapLogger *zap.Logger
		var err error

		if cfg.Mode == "prod" {
			zapLogger, err = zap.NewProduction()
		} else {
			zapLogger, err = zap.NewDevelopment()
		}

		if err != nil {
			return nil, err
		}

		return zapLogger.Sugar(), nil
	})

	// Provide database
	switch cfg.DBType {
	case "postgres", "postgresql", "mysql", "sqlite":
		container.Provide(infra.ProvideSQLDB)
	case "mongo", "mongodb":
		container.Provide(infra.ProvideMongoDB)
	default:
		log.Fatalf("Unsupported DB_TYPE: %s", cfg.DBType)
	}

	// Provide Redis infrastructure
	container.Provide(infra.ProvideRedisClient)
	container.Provide(infra.ProvideRedisEventBus)

	// Provide queue infrastructure for worker
	container.Provide(infra.ProvideAsynqServer)

	// Register module dependencies
	events.RegisterDependencies(container)
	heartbeat.RegisterDependencies(container, &cfg)
	monitor.RegisterDependencies(container, &cfg)
	healthcheck.RegisterDependencies(container)
	monitor_notification.RegisterDependencies(container, &cfg)
	notification_channel.RegisterDependencies(container, &cfg)
	notification_sent_history.RegisterDependencies(container, &cfg)
	monitor_tag.RegisterDependencies(container, &cfg)
	monitor_tls_info.RegisterDependencies(container, &cfg)
	certificate.RegisterDependencies(container)
	monitor_maintenance.RegisterDependencies(container, &cfg)
	maintenance.RegisterDependencies(container, &cfg)
	proxy.RegisterDependencies(container, &cfg)
	stats.RegisterDependencies(container, &cfg)
	setting.RegisterDependencies(container, &cfg)

	// Register worker dependencies
	worker.RegisterDependencies(container)

	// Start the worker
	err = container.Invoke(func(
		w *worker.Worker,
		eventBus events.EventBus,
		logger *zap.SugaredLogger,
	) error {
		// Start the worker
		ctx := context.Background()
		if err := w.Start(ctx); err != nil {
			return fmt.Errorf("failed to start worker: %w", err)
		}

		logger.Info("Worker started successfully")

		// Wait for termination signal
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		logger.Info("Worker is running. Press Ctrl+C to stop.")
		<-sigChan

		logger.Info("Shutdown signal received, stopping worker...")
		w.Stop()

		// Close event bus
		if err := eventBus.Close(); err != nil {
			logger.Errorw("Failed to close event bus", "error", err)
		}

		logger.Info("Worker stopped gracefully")

		return nil
	})

	if err != nil {
		log.Fatalf("Worker error: %v", err)
	}
}
