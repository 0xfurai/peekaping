package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
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
	graceful "peekaping/internal/shutdown"
	"peekaping/internal/utils"
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

	logger, err := internal.ProvideLogger(cfg.ToInternalConfig())
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	address := listenAddress(cfg.Port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		logger.Fatalw("Failed to listen", "address", address, "error", err)
	}

	ctx, stop := graceful.NotifySignals(logger, os.Exit)
	defer stop()

	if err := run(ctx, cfg, logger, listener); err != nil {
		logger.Fatalw("The API stopped with an error", "error", err)
	}
}

// listenAddress is the address the API listens on: the port, 8084 when unset
func listenAddress(port string) string {
	if port == "" {
		port = "8084"
	}
	if port[0] != ':' {
		port = ":" + port
	}
	return port
}

// parts are what run starts and stops
type parts struct {
	dig.In

	Server   *internal.Server
	EventBus events.EventBus

	Settings             setting.Service
	Heartbeats           heartbeat.Service
	Users                auth.Repository
	Stats                stats.Service
	NotificationHistory  notification_sent_history.Service
	TLSInfo              monitor_tls_info.Service
	CleanupLock          cleanup.RunLock
	Monitors             monitor.Service
	Consumer             *notification_channel.QueueConsumer
	NotificationListener *notification_channel.NotificationEventListener
	MonitorListener      *monitor.MonitorEventListener
	Metrics              *metrics.Collector
	UsagePing            *telemetry.Sender
}

// run starts the API and serves it on the listener until ctx is done.
func run(ctx context.Context, cfg *Config, logger *zap.SugaredLogger, listener net.Listener) error {
	internalCfg := cfg.ToInternalConfig()
	container := dig.New()
	if err := app.RegisterClusterAPI(container, internalCfg); err != nil {
		return fmt.Errorf("failed to register dependencies: %w", err)
	}
	// Every module logs through the caller's logger
	if err := container.Decorate(func() *zap.SugaredLogger { return logger }); err != nil {
		return fmt.Errorf("failed to decorate the logger: %w", err)
	}
	// Error reports stay off until the admin's choice is applied below. The reports still queued
	// go out as the process ends.
	if err := app.SetupErrorReporting(container, internalCfg); err != nil {
		return fmt.Errorf("failed to set up error reporting: %w", err)
	}
	defer reporting.Flush(errorReportingFlush)
	var p parts
	if err := container.Invoke(func(resolved parts) { p = resolved }); err != nil {
		return fmt.Errorf("failed to build the API: %w", err)
	}

	// Start the hourly cleanup. Every api replica schedules it and the run lock lets one of them
	// run each hour. A failure to schedule it stops startup.
	retention := cleanup.Retention{
		NotificationHistoryDays: internalCfg.NotificationHistoryRetentionDays,
		TLSInfoDays:             internalCfg.TLSInfoRetentionDays,
	}
	if _, err := cleanup.StartCleanupCron(p.Heartbeats, p.Users, p.Settings, p.Stats, p.NotificationHistory, p.TLSInfo, p.CleanupLock, retention, logger); err != nil {
		return fmt.Errorf("failed to start the hourly cleanup: %w", err)
	}

	// Initialize JWT settings
	if err := p.Settings.InitializeSettings(context.Background()); err != nil {
		return fmt.Errorf("failed to initialize JWT settings: %w", err)
	}

	// Apply the admin's choice from before the restart, which also copies it to Redis for the
	// producer, the worker and the ingester. Then apply it again every minute, so that a copy
	// that missed a Redis that was down is made up within a minute, and a change made on another
	// api replica reaches this one. The stored setting is the source of truth; Redis only
	// carries it. A failure leaves error reports off.
	if err := app.ApplyErrorReporting(context.Background(), container); err != nil {
		logger.Warnw("Failed to apply the error reporting choice, leaving error reports off", "error", err)
	}
	errorReportingCtx, stopErrorReporting := context.WithCancel(context.Background())
	defer stopErrorReporting()
	if _, err := app.StartErrorReportingPublisher(errorReportingCtx, container, redissync.DefaultEvery); err != nil {
		return fmt.Errorf("failed to keep the error reporting choice in Redis: %w", err)
	}

	p.NotificationListener.Subscribe(p.EventBus)

	// Start the monitor event listener
	p.MonitorListener.Subscribe(p.EventBus)

	// Take the notification events the ingester and the producer's watchdog queue. Each goes to
	// one api replica, which hands it to the listener above, so it starts after the listener
	// subscribed: an event handed to a bus with no handler for its topic is dropped.
	if err := p.Consumer.Start(); err != nil {
		return fmt.Errorf("failed to start taking notifications from the queue: %w", err)
	}

	// Repair monitors whose stored status differs from their latest heartbeat. It runs beside
	// startup and only logs, so a failure never keeps the API from serving. Shutdown stops it
	// before the database closes.
	repairCtx, stopRepair := context.WithCancel(context.Background())
	defer stopRepair()
	reporting.Go("repair", func() {
		repaired, err := p.Monitors.RepairStoredStatuses(repairCtx)
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

	// Start the Prometheus metrics collector
	metricsCtx, stopMetrics := context.WithCancel(context.Background())
	defer stopMetrics()
	if internalCfg.MetricsEnabled {
		p.Metrics.Start(metricsCtx, p.EventBus)
	}

	// Start the usage ping sender. It sends nothing until an admin opts in, and a failed ping is
	// dropped. Of the api replicas, the one that wins the claim in Redis sends the day's ping.
	usagePingCtx, stopUsagePing := context.WithCancel(context.Background())
	defer stopUsagePing()
	usagePingDone := p.UsagePing.Start(usagePingCtx, telemetry.DefaultSchedule)

	// Start the server
	go func() {
		logger.Infof("Serving the API on %s", listener.Addr())
		if err := httpx.NewServer("", p.Server.Router, httpx.DefaultTimeouts()).Serve(listener); err != nil {
			logger.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	logger.Info("Shutdown signal received, starting graceful shutdown...")
	stopMetrics()
	stopRepair()
	// The usage ping reads the settings, so it stops before the database closes
	stopUsagePing()
	<-usagePingDone
	// Stop taking notification events before the event bus closes, so none finds it closed
	p.Consumer.Stop()
	// Close event bus: queued events get a few seconds to finish
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 10*time.Second)
	if err := p.EventBus.Close(closeCtx); err != nil {
		logger.Errorw("Failed to close event bus", "error", err)
	}
	cancelClose()

	// Perform graceful database shutdown
	if err := infra.GracefulDatabaseShutdown(container, internalCfg, logger); err != nil {
		logger.Errorw("Failed to shutdown database", "error", err)
	}

	logger.Info("Server stopped gracefully")
	return nil
}
