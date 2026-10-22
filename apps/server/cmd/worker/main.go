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
	"peekaping/internal/reporting"
	"peekaping/internal/reporting/redissync"
	"peekaping/internal/version"
	"syscall"
	"time"
	_ "time/tzdata" // embed the zone database so TZ works in images without tzdata

	"go.uber.org/dig"
	"go.uber.org/zap"
)

// errorReportingFlush is how long the shutdown waits for the error reports still queued
const errorReportingFlush = 2 * time.Second

func main() {
	// A panic that ends the process is reported before it does, if the admin opted in
	defer reporting.ReportCrash("main")

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

	// Error reports stay off until the api's copy of the admin's choice is read from Redis, now
	// and every minute: the worker has no database to read the setting from. The reports still
	// queued go out as the process ends.
	if err := app.SetupErrorReporting(container, internalCfg); err != nil {
		log.Fatalf("Failed to set up error reporting: %v", err)
	}
	defer reporting.Flush(errorReportingFlush)
	errorReportingCtx, stopErrorReporting := context.WithCancel(context.Background())
	defer stopErrorReporting()
	if _, err := app.StartErrorReportingRefresher(errorReportingCtx, container, redissync.DefaultEvery); err != nil {
		log.Fatalf("Failed to follow the error reporting choice: %v", err)
	}

	// Start the worker
	err = container.Invoke(func(
		w *worker.Worker,
		eventBus events.EventBus,
		logger *zap.SugaredLogger,
	) error {
		// Start the worker
		if err := w.Start(); err != nil {
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
