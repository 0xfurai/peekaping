package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"peekaping/internal/app"
	"peekaping/internal/infra"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/producer"
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

	log.Printf("Starting Peekaping Producer v%s", version.Version)

	cfg, err := LoadAndValidate("../..")
	if err != nil {
		log.Fatalf("Failed to load and validate Producer config: %v", err)
	}

	os.Setenv("TZ", cfg.Timezone)

	container := dig.New()

	internalCfg := cfg.ToInternalConfig()

	if err := app.RegisterClusterProducer(container, internalCfg); err != nil {
		log.Fatalf("Failed to register Producer dependencies: %v", err)
	}

	// Error reports stay off until the api's copy of the admin's choice is read from Redis, now
	// and every minute. The reports still queued go out as the process ends.
	if err := app.SetupErrorReporting(container, internalCfg); err != nil {
		log.Fatalf("Failed to set up error reporting: %v", err)
	}
	defer reporting.Flush(errorReportingFlush)
	errorReportingCtx, stopErrorReporting := context.WithCancel(context.Background())
	defer stopErrorReporting()
	if _, err := app.StartErrorReportingRefresher(errorReportingCtx, container, redissync.DefaultEvery); err != nil {
		log.Fatalf("Failed to follow the error reporting choice: %v", err)
	}

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
		if err := prod.Start(context.Background()); err != nil {
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

		// Close event bus: queued events get a few seconds to finish
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 10*time.Second)
		if err := eventBus.Close(closeCtx); err != nil {
			logger.Errorw("Failed to close event bus", "error", err)
		}
		cancelClose()

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
