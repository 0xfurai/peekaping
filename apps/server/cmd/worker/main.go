package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"vigi/internal"
	"vigi/internal/config"
	"vigi/internal/infra"
	"vigi/internal/modules/events"
	"vigi/internal/modules/healthcheck"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/worker"
	"vigi/internal/version"
	"syscall"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func main() {
	log.Printf("Starting Peekaping Worker v%s", version.Version)

	// Load and validate Worker-specific config
	cfg, err := LoadAndValidate("../..")
	if err != nil {
		log.Fatalf("Failed to load and validate Worker config: %v", err)
	}

	// Set timezone
	os.Setenv("TZ", cfg.Timezone)

	// Create DI container
	container := dig.New()

	// Convert to internal config format for dependency injection
	internalCfg := cfg.ToInternalConfig()

	// Provide configuration
	container.Provide(func() *config.Config { return internalCfg })

	// Provide logger
	container.Provide(internal.ProvideLogger)

	// Provide Redis infrastructure
	container.Provide(infra.ProvideRedisClient)
	container.Provide(infra.ProvideRedisEventBus)

	// Provide queue infrastructure for worker
	container.Provide(infra.ProvideAsynqClient)
	container.Provide(infra.ProvideAsynqServer)
	container.Provide(infra.ProvideAsynqInspector)
	container.Provide(infra.ProvideQueueService)

	// Register only non-database module dependencies needed for health checks
	healthcheck.RegisterDependencies(container)
	heartbeat.RegisterDependencies(container, internalCfg)

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
