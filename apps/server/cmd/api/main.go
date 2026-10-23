// Command api runs the HTTP API and the websocket of Peekaping in cluster mode (ADR 0002),
// beside cmd/producer, cmd/worker and cmd/ingester. It also takes the notification events the
// ingester and the producer's watchdog queue, and runs the cleanup cron and the status repair.
//
// It doesn't migrate the database: the startup script runs the migrations with cmd/bun before
// it starts the server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"
	_ "time/tzdata" // embed the zone database so TZ works in images without tzdata

	"peekaping/internal"
	"peekaping/internal/app"
	"peekaping/internal/config"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/cleanup"
	"peekaping/internal/modules/error_reporting"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/metrics"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/telemetry"
	"peekaping/internal/modules/websocket"
	"peekaping/internal/reporting"
	"peekaping/internal/reporting/redissync"
	graceful "peekaping/internal/shutdown" // aliased: this package has a shutdown function
	"peekaping/internal/utils"
	"peekaping/internal/version"

	"github.com/robfig/cron/v3"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// serverTimeouts are how long the HTTP server waits on a client
var serverTimeouts = httpx.DefaultTimeouts()

// errorReportingFlush is how long the shutdown waits for the error reports still queued
const errorReportingFlush = 2 * time.Second

// shutdownTimeouts are the API's, 30 seconds in all, Kubernetes' default grace period. A test
// may shorten them.
var shutdownTimeouts = graceful.Timeouts{HTTP: 15 * time.Second, Jobs: 10 * time.Second, EventBus: 5 * time.Second}

func main() {
	// A panic that ends the process is reported before it does, unless the admin opted out. Reports
	// start only once app.ApplyErrorReporting has applied the stored choice (below), so a panic
	// before that is not reported.
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

	Server    *internal.Server
	WebSocket *websocket.Server
	Consumer  *notification_channel.QueueConsumer
	EventBus  events.EventBus
	Database  graceful.Database

	Settings             setting.Service
	Heartbeats           heartbeat.Service
	Users                auth.Repository
	Stats                stats.Service
	NotificationHistory  notification_sent_history.Service
	TLSInfo              monitor_tls_info.Service
	CleanupLock          cleanup.RunLock
	Monitors             monitor.Service
	Probes               probe.Service
	NotificationListener *notification_channel.NotificationEventListener
	MonitorListener      *monitor.MonitorEventListener
	Metrics              *metrics.Collector
	UsagePing            *telemetry.Sender
	ErrorReporting       error_reporting.Service
}

// run starts the API and serves it on the listener until ctx is done, or until serving fails.
// Then it stops everything that started, in order, within shutdownTimeouts: the HTTP server,
// the websocket, the background jobs, the event bus and the database. It returns nil when
// serving ended with ctx and everything stopped in time. It closes the listener.
func run(ctx context.Context, cfg *Config, logger *zap.SugaredLogger, listener net.Listener) error {
	// The HTTP server closes the listener once it serves, and a failure before leaves it open
	defer func() { _ = listener.Close() }()

	logger.Infow("Starting the API in cluster mode", "version", version.Version)
	internalCfg := cfg.ToInternalConfig()
	container := dig.New()
	if err := app.RegisterClusterAPI(container, internalCfg); err != nil {
		return fmt.Errorf("failed to register dependencies: %w", err)
	}
	// Every module logs through the caller's logger
	if err := container.Decorate(func() *zap.SugaredLogger { return logger }); err != nil {
		return fmt.Errorf("failed to decorate the logger: %w", err)
	}
	// Error reports stay off until startJobs applies the admin's choice. The reports still queued
	// go out as the process ends.
	if err := app.SetupErrorReporting(container, internalCfg); err != nil {
		return fmt.Errorf("failed to set up error reporting: %w", err)
	}
	defer reporting.Flush(errorReportingFlush)
	var p parts
	if err := container.Invoke(func(resolved parts) { p = resolved }); err != nil {
		return fmt.Errorf("failed to build the API: %w", err)
	}

	// Counted, so that the database closes only once every handler has returned: the HTTP
	// server's Close drops the connections still open, but doesn't wait for their handlers
	handlers := graceful.NewRunningHandlers(p.Server.Router)
	httpServer := httpx.NewServer("", handlers, serverTimeouts)
	// The websocket server closes as soon as the HTTP server's Shutdown stops taking connections.
	// Closing its sessions answers their open long polls, which Shutdown waits for like any
	// request in flight: left open, they'd hold it up until the clients' next ping, 25 seconds
	// away. They are answered while their handlers still wait on them, which engine.io handles
	// safely, unlike a long poll whose connection was already dropped.
	websocketClosed := make(chan struct{})
	httpServer.RegisterOnShutdown(func() {
		defer close(websocketClosed)
		p.WebSocket.Close()
	})

	// A job that fails to start leaves the ones before it running, and the shutdown stops them
	j, startErr := startJobs(ctx, p, internalCfg, logger)
	var failure error
	if startErr != nil {
		failure = startErr
		logger.Errorw("Failed to start the API, shutting down", "error", startErr)
	} else {
		served := make(chan error, 1)
		go func() {
			logger.Infow("Serving the API", "address", listener.Addr().String())
			served <- httpServer.Serve(listener)
		}()

		// Serve returns http.ErrServerClosed once the shutdown below has begun, when nothing
		// reads it any more: here it can only be a failure
		select {
		case <-ctx.Done():
			logger.Info("Shutdown signal received, shutting down")
		case serveErr := <-served:
			failure = fmt.Errorf("HTTP server failed: %w", serveErr)
			logger.Errorw("HTTP server failed, shutting down", "error", failure)
		}
	}

	if err := errors.Join(failure, shutdown(httpServer, handlers, websocketClosed, p, j, internalCfg.DBType, logger)); err != nil {
		return err
	}
	logger.Info("API stopped")
	return nil
}

// jobs are the background jobs run starts, and how to stop them. A field is nil until its job
// has started.
type jobs struct {
	consumer           *notification_channel.QueueConsumer
	cleanup            *cron.Cron
	stopErrorReporting context.CancelFunc
	errorReportingDone chan struct{}
	stopRepair         context.CancelFunc
	repairDone         chan struct{}
	stopMetrics        context.CancelFunc
	metricsDone        <-chan struct{}
	stopUsagePing      context.CancelFunc
	usagePingDone      <-chan struct{}
}

// startJobs starts the cleanup cron, the settings, the local probe's token, the error reporting
// choice, the listeners, the notification queue, the status repair, the metrics collector and the
// usage ping. The notification listener subscribes before the queue consumer starts. When one fails, it returns
// the jobs that had started, to stop.
func startJobs(ctx context.Context, p parts, cfg *config.Config, logger *zap.SugaredLogger) (*jobs, error) {
	j := &jobs{}
	// Every api replica schedules the hourly cleanup, and the run lock lets one of them run each
	// hour
	retention := cleanup.Retention{
		NotificationHistoryDays: cfg.NotificationHistoryRetentionDays,
		TLSInfoDays:             cfg.TLSInfoRetentionDays,
	}
	hourly, err := cleanup.StartCleanupCron(p.Heartbeats, p.Users, p.Settings, p.Stats, p.NotificationHistory, p.TLSInfo, p.CleanupLock, retention, logger)
	if err != nil {
		return j, fmt.Errorf("failed to start the hourly cleanup: %w", err)
	}
	j.cleanup = hourly

	if err := p.Settings.InitializeSettings(ctx); err != nil {
		return j, fmt.Errorf("failed to initialize JWT settings: %w", err)
	}

	// PROBE_BOOTSTRAP_TOKEN becomes the local probe's token, so that a stack comes up with a probe
	// token and no UI step. A token that can't be stored stops startup.
	if err := app.BootstrapLocalProbeToken(ctx, p.Probes, cfg.ProbeBootstrapToken, logger); err != nil {
		return j, fmt.Errorf("failed to bootstrap the local probe: %w", err)
	}

	// Apply the admin's choice from before the restart, which also copies it to Redis for the
	// producer, the worker and the ingester. Then apply it again every minute, so that a copy
	// that missed a Redis that was down is made up within a minute, and a change made on another
	// api replica reaches this one. The stored setting is the source of truth; Redis only
	// carries it. A failure leaves error reports off.
	if err := p.ErrorReporting.Apply(ctx); err != nil {
		logger.Warnw("Failed to apply the error reporting choice, leaving error reports off", "error", err)
	}
	var errorReportingCtx context.Context
	errorReportingCtx, j.stopErrorReporting = context.WithCancel(context.Background())
	j.errorReportingDone = make(chan struct{})
	reporting.Go("error-reporting", func() {
		defer close(j.errorReportingDone)
		p.ErrorReporting.Run(errorReportingCtx, redissync.DefaultEvery)
	})

	p.NotificationListener.Subscribe(p.EventBus)
	p.MonitorListener.Subscribe(p.EventBus)

	// Take the notification events the ingester and the producer's watchdog queue. Each goes to
	// one api replica, which hands it to the listener above, so it starts after the listener
	// subscribed: an event handed to a bus with no handler for its topic is dropped.
	if err := p.Consumer.Start(); err != nil {
		return j, fmt.Errorf("failed to start taking notifications from the queue: %w", err)
	}
	j.consumer = p.Consumer

	// Repair monitors whose stored status differs from their latest heartbeat. It runs beside
	// startup and only logs, so a failure never keeps the API from serving.
	var repairCtx context.Context
	repairCtx, j.stopRepair = context.WithCancel(context.Background())
	j.repairDone = make(chan struct{})
	reporting.Go("repair", func() {
		defer close(j.repairDone)
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

	if cfg.MetricsEnabled {
		var metricsCtx context.Context
		metricsCtx, j.stopMetrics = context.WithCancel(context.Background())
		j.metricsDone = p.Metrics.Start(metricsCtx, p.EventBus)
	}

	// The usage ping sends nothing once an admin opted out or while the environment forces the
	// ping off, and a failed one is dropped. Of the api replicas, the one that wins the claim in
	// Redis sends the day's ping.
	var usagePingCtx context.Context
	usagePingCtx, j.stopUsagePing = context.WithCancel(context.Background())
	j.usagePingDone = p.UsagePing.Start(usagePingCtx, telemetry.DefaultSchedule)

	return j, nil
}

// stop stops the background jobs and waits for them until ctx is done. The notification queue
// stops first, so that no task finds the event bus closed. Each step that stops is logged.
func (j *jobs) stop(ctx context.Context, logger *zap.SugaredLogger) error {
	var errs []error
	await := func(what string, done <-chan struct{}) {
		if err := graceful.Await(ctx, what, done); err != nil {
			errs = append(errs, err)
			return
		}
		logger.Infof("Stopped %s", what)
	}

	if j.consumer != nil {
		consumerDone := make(chan struct{})
		go func() {
			defer close(consumerDone)
			j.consumer.Stop()
		}()
		await("the notification queue", consumerDone)
	}
	if j.cleanup != nil {
		await("the cleanup cron", j.cleanup.Stop().Done())
	}
	if j.stopRepair != nil {
		j.stopRepair()
		await("the status repair", j.repairDone)
	}
	if j.stopMetrics != nil {
		j.stopMetrics()
		await("the metrics collector", j.metricsDone)
	}
	// Both read the settings, so they stop before the database closes
	if j.stopUsagePing != nil {
		j.stopUsagePing()
		await("the usage ping", j.usagePingDone)
	}
	if j.stopErrorReporting != nil {
		j.stopErrorReporting()
		await("the error reporting choice's copy to Redis", j.errorReportingDone)
	}
	return errors.Join(errs...)
}

// shutdown stops everything in order, each step within its share of shutdownTimeouts. The HTTP
// server stops taking requests and waits for those in flight; once its share is up, it drops
// their connections. The websocket closes its sessions, from the HTTP server's Shutdown
// (websocketClosed is closed once it has). The background jobs stop, so nothing publishes events
// any more, and the HTTP handlers the HTTP server left running return, in the jobs' share. The
// event bus lets the handlers of the queued events finish. The database closes last, and only
// once nothing uses it.
func shutdown(httpServer *http.Server, handlers *graceful.RunningHandlers, websocketClosed <-chan struct{}, p parts, j *jobs, dbType string, logger *zap.SugaredLogger) error {
	var errs []error

	httpCtx, cancelHTTP := context.WithTimeout(context.Background(), shutdownTimeouts.HTTP)
	defer cancelHTTP()
	if err := httpServer.Shutdown(httpCtx); err != nil {
		errs = append(errs, fmt.Errorf("failed to shut down the HTTP server: %w", err))
		// Drop the connections still open, so that the shutdown goes on. Their handlers may run
		// on, on a write that waits for the database for instance.
		_ = httpServer.Close()
	}
	logger.Info("Stopped the HTTP server")

	<-websocketClosed
	logger.Info("Closed the websocket server")

	// What uses the database and still runs once its share is up
	var running []string

	jobsCtx, cancelJobs := context.WithTimeout(context.Background(), shutdownTimeouts.Jobs)
	defer cancelJobs()
	if err := j.stop(jobsCtx, logger); err != nil {
		errs = append(errs, err)
		running = append(running, "background jobs")
	}
	// The HTTP handlers the HTTP server left running return in the jobs' share too, before the
	// event bus closes, so that what they publish reaches it
	if err := graceful.Await(jobsCtx, "the HTTP handlers", handlers.Done()); err != nil {
		errs = append(errs, err)
		running = append(running, "HTTP handlers")
	}

	busCtx, cancelBus := context.WithTimeout(context.Background(), shutdownTimeouts.EventBus)
	defer cancelBus()
	if err := p.EventBus.Close(busCtx); err != nil {
		errs = append(errs, fmt.Errorf("failed to close the event bus: %w", err))
		running = append(running, "event handlers")
	} else {
		logger.Info("Closed the event bus")
	}

	// Closing the database under a job, an HTTP handler or an event handler would fail it halfway
	// through its writes. The process's exit releases the database instead.
	if len(running) > 0 {
		logger.Errorw("Left the database open, since some of what uses it is still running", "running", running)
		return errors.Join(errs...)
	}
	// In what is left of the event bus's share, so that a slow MongoDB doesn't hold up the exit
	if err := p.Database.Close(busCtx, dbType, logger); err != nil {
		errs = append(errs, err)
	} else {
		logger.Info("Closed the database")
	}

	return errors.Join(errs...)
}
