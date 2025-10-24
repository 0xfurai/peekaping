package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"peekaping/internal"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/monitor_maintenance"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stats"
	"peekaping/internal/version"
	"syscall"

	"github.com/hibiken/asynq"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

func main() {
	log.Printf("Starting Peekaping Ingester v%s", version.Version)

	// Load and validate Ingester-specific config
	cfg, err := LoadAndValidate("../..")
	if err != nil {
		log.Fatalf("Failed to load and validate Ingester config: %v", err)
	}

	// Set timezone
	os.Setenv("TZ", cfg.Timezone)

	// Create DI container
	container := dig.New()

	// Convert to internal config format for dependency injection
	internalCfg := cfg.ToInternalConfig()

	// Provide configuration
	container.Provide(func() *config.Config { return internalCfg })

	container.Provide(internal.ProvideLogger)

	// Provide database
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

	// Provide queue infrastructure for ingester
	// For ingester, we only need the server to consume tasks
	container.Provide(func(cfg *config.Config, logger *zap.SugaredLogger) (*asynq.Server, error) {
		redisOpt := asynq.RedisClientOpt{
			Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		}

		// Configure server with appropriate concurrency and queue priorities
		// Ingester only processes tasks from the "ingester" queue
		serverCfg := asynq.Config{
			Concurrency: cfg.QueueConcurrency,
			Queues: map[string]int{
				"ingester": 10, // Only process ingester queue
			},
			ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
				logger.Errorw("Task processing failed",
					"type", task.Type(),
					"payload", string(task.Payload()),
					"error", err,
				)
			}),
			StrictPriority: false, // We only have one queue, no need for strict priority
			Logger:         infra.NewAsynqLogger(logger),
		}

		server := asynq.NewServer(redisOpt, serverCfg)

		logger.Info("Successfully created Asynq server for ingester")
		return server, nil
	})

	// Register module dependencies
	heartbeat.RegisterDependencies(container, internalCfg)
	notification_sent_history.RegisterDependencies(container, internalCfg)
	monitor_tls_info.RegisterDependencies(container, internalCfg)
	certificate.RegisterDependencies(container)
	monitor_maintenance.RegisterDependencies(container, internalCfg)
	stats.RegisterDependencies(container, internalCfg)
	setting.RegisterDependencies(container, internalCfg)

	// Register ingester dependencies
	ingester.RegisterDependencies(container)

	// Start the ingester
	err = container.Invoke(func(
		ing *ingester.Ingester,
		eventBus events.EventBus,
		logger *zap.SugaredLogger,
	) error {
		// Start the ingester
		ctx := context.Background()
		if err := ing.Start(ctx); err != nil {
			return fmt.Errorf("failed to start ingester: %w", err)
		}

		logger.Info("Ingester started successfully")

		// Wait for termination signal
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		logger.Info("Ingester is running. Press Ctrl+C to stop.")
		<-sigChan

		logger.Info("Shutdown signal received, stopping ingester...")
		ing.Stop()

		// Close event bus
		if err := eventBus.Close(); err != nil {
			logger.Errorw("Failed to close event bus", "error", err)
		}

		logger.Info("Ingester stopped gracefully")

		return nil
	})

	if err != nil {
		log.Fatalf("Ingester error: %v", err)
	}
}
