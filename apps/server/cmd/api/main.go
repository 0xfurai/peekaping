package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"peekaping/internal"
	"peekaping/internal/app"
	"peekaping/internal/httpx"
	"peekaping/internal/infra"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/cleanup"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/metrics"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/telemetry"
	"peekaping/internal/reporting"
	"peekaping/internal/reporting/redissync"
	"peekaping/internal/utils"
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

	if err := app.RegisterClusterAPI(container, internalCfg); err != nil {
		log.Fatalf("Failed to register API dependencies: %v", err)
	}

	// Error reports stay off until the admin's choice is applied below. The reports still queued
	// go out as the process ends.
	if err := app.SetupErrorReporting(container, internalCfg); err != nil {
		log.Fatalf("Failed to set up error reporting: %v", err)
	}
	defer reporting.Flush(errorReportingFlush)

	// Start the hourly cleanup. Every api replica schedules it and the run lock lets one of them
	// run each hour. A failure to schedule it stops startup.
	err = container.Invoke(func(
		heartbeatService heartbeat.Service,
		userRepository auth.Repository,
		settingService setting.Service,
		statsService stats.Service,
		notificationHistoryService notification_sent_history.Service,
		tlsInfoService monitor_tls_info.Service,
		runLock cleanup.RunLock,
		logger *zap.SugaredLogger,
	) error {
		retention := cleanup.Retention{
			NotificationHistoryDays: internalCfg.NotificationHistoryRetentionDays,
			TLSInfoDays:             internalCfg.TLSInfoRetentionDays,
		}
		_, err := cleanup.StartCleanupCron(heartbeatService, userRepository, settingService, statsService, notificationHistoryService, tlsInfoService, runLock, retention, logger)
		return err
	})
	if err != nil {
		log.Fatalf("Failed to start the hourly cleanup: %v", err)
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

	// PROBE_BOOTSTRAP_TOKEN becomes the local probe's token, so that a stack comes up with a probe
	// token and no UI step. A token that can't be stored stops startup.
	if err := app.BootstrapLocalProbe(context.Background(), container); err != nil {
		log.Fatalf("Failed to bootstrap the local probe: %v", err)
	}

	// Apply the admin's choice from before the restart, which also copies it to Redis for the
	// producer, the worker and the ingester. Then apply it again every minute, so that a copy
	// that missed a Redis that was down is made up within a minute, and a change made on another
	// api replica reaches this one. The stored setting is the source of truth; Redis only
	// carries it. A failure leaves error reports off.
	if err := app.ApplyErrorReporting(context.Background(), container); err != nil {
		log.Printf("Failed to apply the error reporting choice, leaving error reports off: %v", err)
	}
	errorReportingCtx, stopErrorReporting := context.WithCancel(context.Background())
	defer stopErrorReporting()
	if _, err := app.StartErrorReportingPublisher(errorReportingCtx, container, redissync.DefaultEvery); err != nil {
		log.Fatalf("Failed to keep the error reporting choice in Redis: %v", err)
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

	// Take the notification events the ingester and the producer's watchdog queue. Each goes to
	// one api replica, which hands it to the listener above, so it starts after the listener
	// subscribed: an event handed to a bus with no handler for its topic is dropped.
	err = container.Invoke(func(consumer *notification_channel.QueueConsumer) error {
		return consumer.Start()
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
		reporting.Go("repair", func() {
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
		})
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

	// Start the usage ping sender. It sends nothing until an admin opts in, and a failed ping is
	// dropped. Of the api replicas, the one that wins the claim in Redis sends the day's ping.
	usagePingCtx, stopUsagePing := context.WithCancel(context.Background())
	defer stopUsagePing()
	var usagePingDone <-chan struct{}
	err = container.Invoke(func(sender *telemetry.Sender) {
		usagePingDone = sender.Start(usagePingCtx, telemetry.DefaultSchedule)
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start the server with graceful shutdown
	err = container.Invoke(func(
		server *internal.Server,
		consumer *notification_channel.QueueConsumer,
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
			if err := httpx.NewServer(port, server.Router, httpx.DefaultTimeouts()).ListenAndServe(); err != nil {
				logger.Fatalf("Failed to start server: %v", err)
			}
		}()

		// Wait for shutdown signal
		<-sigChan
		logger.Info("Shutdown signal received, starting graceful shutdown...")
		stopMetrics()
		stopRepair()
		// The usage ping reads the settings, so it stops before the database closes
		stopUsagePing()
		<-usagePingDone
		// Stop taking notification events before the event bus closes, so none finds it closed
		consumer.Stop()
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

		logger.Info("Server stopped gracefully")
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}
}
