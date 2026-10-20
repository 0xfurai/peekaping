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
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/stats"
	"peekaping/internal/version"
	"syscall"
	"time"
	_ "time/tzdata" // embed the zone database so TZ works in images without tzdata

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

	if err := app.RegisterClusterIngester(container, internalCfg); err != nil {
		log.Fatalf("Failed to register Ingester dependencies: %v", err)
	}

	err = container.Invoke(func(s stats.Service, bus events.EventBus) {
		if impl, ok := s.(*stats.ServiceImpl); ok {
			impl.RegisterEventHandlers(bus)
		}
	})
	if err != nil {
		log.Fatalf("Failed to register stats event handlers: %v", err)
	}

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

		logger.Info("Ingester stopped gracefully")

		return nil
	})

	if err != nil {
		log.Fatalf("Ingester error: %v", err)
	}
}
