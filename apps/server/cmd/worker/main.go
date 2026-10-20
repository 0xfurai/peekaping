package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"peekaping/internal/app"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/worker"
	"peekaping/internal/version"
	"syscall"
	"time"
	_ "time/tzdata" // embed the zone database so TZ works in images without tzdata

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

	if err := app.RegisterClusterWorker(container, internalCfg); err != nil {
		log.Fatalf("Failed to register Worker dependencies: %v", err)
	}

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

		// Close event bus: queued events get a few seconds to finish
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 10*time.Second)
		if err := eventBus.Close(closeCtx); err != nil {
			logger.Errorw("Failed to close event bus", "error", err)
		}
		cancelClose()

		logger.Info("Worker stopped gracefully")

		return nil
	})

	if err != nil {
		log.Fatalf("Worker error: %v", err)
	}
}
