package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"peekaping/internal"
	"peekaping/internal/infra"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/cleanup"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/metrics"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stats"
	"peekaping/internal/utils"
	"syscall"
	_ "time/tzdata" // embed the zone database so TZ works in images without tzdata

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func main() {

	utils.RegisterCustomValidators()

	// Load and validate API-specific config
	cfg, err := LoadAndValidate("../..")
	if err != nil {
		log.Fatalf("Failed to load and validate API config: %v", err)
	}

	os.Setenv("TZ", cfg.Timezone)

	container := dig.New()

	// Convert to internal config format for dependency injection
	internalCfg := cfg.ToInternalConfig()

	if err := internal.RegisterAPIDependencies(container, internalCfg); err != nil {
		log.Fatal(err)
	}

	// Start the event healthcheck listener
	err = container.Invoke(func(listener *healthcheck.EventListener, eventBus events.EventBus) {
		listener.Start(eventBus)
	})

	if err != nil {
		log.Fatal(err)
	}

	// Start cleanup cron job(s)
	err = container.Invoke(func(
		heartbeatService heartbeat.Service,
		userRepository auth.Repository,
		settingService setting.Service,
		statsService stats.Service,
		notificationHistoryService notification_sent_history.Service,
		tlsInfoService monitor_tls_info.Service,
		logger *zap.SugaredLogger,
	) {
		cleanup.StartCleanupCron(heartbeatService, userRepository, settingService, statsService, notificationHistoryService, tlsInfoService, logger)
	})
	if err != nil {
		log.Fatal(err)
	}

	// Initialize JWT settings
	err = container.Invoke(func(settingService setting.Service) {
		if err := settingService.InitializeSettings(context.Background()); err != nil {
			log.Fatalf("Failed to initialize JWT settings: %v", err)
		}
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start the health check supervisor
	// err = container.Invoke(func(supervisor *healthcheck.HealthCheckSupervisor) {
	// 	if err := supervisor.StartAll(context.Background()); err != nil {
	// 		log.Fatal(err)
	// 	}
	// })
	// if err != nil {
	// 	log.Fatal(err)
	// }

	err = container.Invoke(func(listener *notification_channel.NotificationEventListener, eventBus events.EventBus) {
		listener.Subscribe(eventBus)
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start the monitor event listener
	err = container.Invoke(func(listener *monitor.MonitorEventListener, eventBus events.EventBus) {
		listener.Subscribe(eventBus)
	})
	if err != nil {
		log.Fatal(err)
	}

	// Repair monitors whose stored status differs from their latest heartbeat. It runs beside
	// startup and only logs, so a failure never keeps the API from serving. Shutdown stops it
	// before the database closes.
	repairCtx, stopRepair := context.WithCancel(context.Background())
	defer stopRepair()
	err = container.Invoke(func(monitorService monitor.Service, logger *zap.SugaredLogger) {
		go func() {
			repaired, err := monitorService.RepairStoredStatuses(repairCtx)
			if errors.Is(err, context.Canceled) {
				logger.Infow("Stopped repairing monitor statuses on shutdown", "repaired", repaired)
				return
			}
			if err != nil {
				logger.Errorw("Failed to repair some monitor statuses", "repaired", repaired, "error", err)
				return
			}
			logger.Infow("Repaired monitor statuses", "repaired", repaired)
		}()
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start the Prometheus metrics collector
	metricsCtx, stopMetrics := context.WithCancel(context.Background())
	defer stopMetrics()
	if internalCfg.MetricsEnabled {
		err = container.Invoke(func(collector *metrics.Collector, eventBus events.EventBus) {
			collector.Start(metricsCtx, eventBus)
		})
		if err != nil {
			log.Fatal(err)
		}
	}

	// Start the server with graceful shutdown
	err = container.Invoke(func(
		server *internal.Server,
		eventBus events.EventBus,
		logger *zap.SugaredLogger,
	) error {

		port := server.Cfg.Port
		if port == "" {
			port = "8084"
		}
		if port[0] != ':' {
			port = ":" + port
		}

		// Set up signal handling for graceful shutdown
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		// Start server in a goroutine
		go func() {
			logger.Infof("Starting server on port %s", port)
			if err := http.ListenAndServe(port, server.Router); err != nil {
				logger.Fatalf("Failed to start server: %v", err)
			}
		}()

		// Wait for shutdown signal
		<-sigChan
		logger.Info("Shutdown signal received, starting graceful shutdown...")
		stopMetrics()
		stopRepair()
		// Close event bus
		if err := eventBus.Close(); err != nil {
			logger.Errorw("Failed to close event bus", "error", err)
		}

		// Perform graceful database shutdown
		if err := infra.GracefulDatabaseShutdown(container, internalCfg, logger); err != nil {
			logger.Errorw("Failed to shutdown database", "error", err)
		}

		logger.Info("Server stopped gracefully")
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}
}
