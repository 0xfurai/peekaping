package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_maintenance"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/producer"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/stats"
	"peekaping/internal/version"
	"syscall"
	"time"

	"go.uber.org/dig"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	log.Printf("Starting Peekaping Producer v%s", version.Version)

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
			cfg := zap.NewDevelopmentConfig()
			cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel) // filter out Debug
			cfg.EncoderConfig.EncodeTime = zapcore.TimeEncoder(func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
				enc.AppendString("[" + t.Format("15:04:05.000") + "]")
			})
			cfg.EncoderConfig.LevelKey = "" // remove level
			cfg.EncoderConfig.CallerKey = ""
			zapLogger, err = cfg.Build()
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

	// Provide queue infrastructure
	container.Provide(infra.ProvideAsynqClient)
	container.Provide(infra.ProvideAsynqInspector)
	container.Provide(infra.ProvideQueueService)

	// Register module dependencies
	events.RegisterDependencies(container)
	heartbeat.RegisterDependencies(container, &cfg)
	monitor.RegisterDependencies(container, &cfg)
	monitor_maintenance.RegisterDependencies(container, &cfg)
	maintenance.RegisterDependencies(container, &cfg)
	healthcheck.RegisterDependencies(container)
	monitor_notification.RegisterDependencies(container, &cfg)
	monitor_tag.RegisterDependencies(container, &cfg)
	proxy.RegisterDependencies(container, &cfg)
	stats.RegisterDependencies(container, &cfg)
	queue.RegisterDependencies(container, &cfg)

	// Register producer dependencies
	producer.RegisterDependencies(container)

	// Start the producer
	err = container.Invoke(func(
		prod *producer.Producer,
		eventListener *producer.EventListener,
		eventBus events.EventBus,
		logger *zap.SugaredLogger,
	) error {
		// Subscribe to monitor events
		eventListener.Subscribe(eventBus)
		logger.Info("Event listener subscribed to monitor events")

		// Start the producer
		ctx := context.Background()
		if err := prod.Start(ctx); err != nil {
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

		logger.Info("Producer stopped gracefully")

		return nil
	})

	if err != nil {
		log.Fatalf("Producer error: %v", err)
	}
}
