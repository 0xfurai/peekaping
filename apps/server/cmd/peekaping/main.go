// Command peekaping runs Peekaping in single mode (ADR 0002): the API and the websocket, the
// scheduler and its check pool, the ingest and the stale-monitor watchdog, in one process with
// no Redis. Cluster mode runs cmd/api, cmd/producer, cmd/worker and cmd/ingester instead. With
// WEB_DIR set, it serves the web app on the API's port too.
//
// It doesn't migrate the database: the startup script runs the migrations with cmd/bun before
// it starts the server, as it does in cluster mode.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // embed the zone database so TZ works in images without tzdata

	"peekaping/internal"
	"peekaping/internal/app"
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
	"peekaping/internal/modules/scheduler"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/websocket"
	"peekaping/internal/utils"
	"peekaping/internal/version"
	"peekaping/internal/web"

	"github.com/robfig/cron/v3"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

const (
	// shutdownTimeout is how long the shutdown has, from the signal, to stop everything in order
	shutdownTimeout = 30 * time.Second
	// readHeaderTimeout is how long a client has to send a request's headers
	readHeaderTimeout = 10 * time.Second
)

func main() {
	utils.RegisterCustomValidators()

	cfg, err := LoadAndValidate("../..")
	if err != nil {
		log.Fatalf("Failed to load and validate config: %v", err)
	}

	os.Setenv("TZ", cfg.Timezone)

	logger, err := internal.ProvideLogger(cfg.ToInternalConfig())
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	listener, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		logger.Fatalw("Failed to listen", "port", cfg.Port, "error", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg, logger, listener); err != nil {
		logger.Fatalw("Peekaping stopped with an error", "error", err)
	}
}

// parts are what run starts and stops
type parts struct {
	dig.In

	Server    *internal.Server
	WebSocket *websocket.Server
	Scheduler *scheduler.Scheduler
	Watchdog  *stale.Watchdog
	EventBus  events.EventBus
	DB        *bun.DB

	Settings             setting.Service
	Heartbeats           heartbeat.Service
	Users                auth.Repository
	Stats                stats.Service
	NotificationHistory  notification_sent_history.Service
	TLSInfo              monitor_tls_info.Service
	Monitors             monitor.Service
	NotificationListener *notification_channel.NotificationEventListener
	MonitorListener      *monitor.MonitorEventListener
	Metrics              *metrics.Collector
}

// run starts Peekaping and serves the API on the listener until ctx is done. Then it stops
// everything in order, within shutdownTimeout: the HTTP server, the websocket, the background
// jobs, the event bus and the database. It returns nil when everything stopped in time.
func run(ctx context.Context, cfg *Config, logger *zap.SugaredLogger, listener net.Listener) error {
	logger.Infow("Starting Peekaping in single mode", "version", version.Version)
	if ignored := cfg.RedisSettings(); len(ignored) > 0 {
		logger.Warnw("Single mode uses no Redis, ignoring the Redis settings", "settings", ignored)
	}

	container := dig.New()
	if err := app.RegisterSingle(container, cfg.ToInternalConfig()); err != nil {
		return fmt.Errorf("failed to register dependencies: %w", err)
	}
	// Every module logs through the caller's logger
	if err := container.Decorate(func() *zap.SugaredLogger { return logger }); err != nil {
		return fmt.Errorf("failed to decorate the logger: %w", err)
	}
	var p parts
	if err := container.Invoke(func(resolved parts) { p = resolved }); err != nil {
		return fmt.Errorf("failed to build peekaping: %w", err)
	}

	// The API router serves every request. With WEB_DIR set, the gateway sends it the API's
	// paths and serves the web app on the others.
	handler := http.Handler(p.Server.Router)
	if cfg.WebDir != "" {
		gateway, err := web.NewGateway(web.Config{Dir: cfg.WebDir, APIURL: cfg.APIURL, Metrics: cfg.MetricsEnabled}, p.Server.Router, logger)
		if err != nil {
			return fmt.Errorf("failed to serve the web app from WEB_DIR: %w", err)
		}
		handler = gateway
	}

	jobs, err := startJobs(ctx, p, cfg.MetricsEnabled, logger)
	if err != nil {
		return err
	}

	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: readHeaderTimeout}
	served := make(chan error, 1)
	go func() {
		logger.Infow("Serving the API", "address", listener.Addr().String(), "web_dir", cfg.WebDir)
		served <- httpServer.Serve(listener)
	}()

	var serveErr error
	select {
	case <-ctx.Done():
		logger.Info("Shutdown signal received, shutting down")
	case serveErr = <-served:
		serveErr = fmt.Errorf("HTTP server failed: %w", serveErr)
		logger.Errorw("HTTP server failed, shutting down", "error", serveErr)
	}

	if err := errors.Join(serveErr, shutdown(httpServer, p, jobs, cfg.DBType, logger)); err != nil {
		return err
	}
	logger.Info("Peekaping stopped")
	return nil
}

// jobs are the background jobs run starts, and how to stop them
type jobs struct {
	scheduler    *scheduler.Scheduler
	stopWatchdog context.CancelFunc
	watchdogDone chan struct{}
	cleanup      *cron.Cron
	stopRepair   context.CancelFunc
	repairDone   chan struct{}
	stopMetrics  context.CancelFunc
}

// startJobs starts what cmd/api starts beside the server (the cleanup cron, the settings, the
// listeners, the status repair and the metrics collector), the stats aggregation that
// cmd/ingester starts, and the scheduler and its watchdog in place of cmd/producer. Each runs
// once: this process is the only one, and so always the leader.
func startJobs(ctx context.Context, p parts, metricsEnabled bool, logger *zap.SugaredLogger) (*jobs, error) {
	if err := p.Settings.InitializeSettings(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize JWT settings: %w", err)
	}

	j := &jobs{scheduler: p.Scheduler}
	j.cleanup = cleanup.StartCleanupCron(p.Heartbeats, p.Users, p.Settings, p.Stats, p.NotificationHistory, p.TLSInfo, logger)

	p.NotificationListener.Subscribe(p.EventBus)
	p.MonitorListener.Subscribe(p.EventBus)
	if impl, ok := p.Stats.(*stats.ServiceImpl); ok {
		impl.RegisterEventHandlers(p.EventBus)
	}

	// Repair monitors whose stored status differs from their latest heartbeat. It runs beside
	// startup and only logs, so a failure never keeps the API from serving.
	var repairCtx context.Context
	repairCtx, j.stopRepair = context.WithCancel(context.Background())
	j.repairDone = make(chan struct{})
	go func() {
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
	}()

	var metricsCtx context.Context
	metricsCtx, j.stopMetrics = context.WithCancel(context.Background())
	if metricsEnabled {
		p.Metrics.Start(metricsCtx, p.EventBus)
	}

	// The scheduler and the watchdog run until the shutdown stops them, not until ctx is done,
	// so that they stop after the HTTP server
	p.Scheduler.Subscribe(p.EventBus)
	if err := p.Scheduler.Start(context.Background()); err != nil {
		j.stopRepair()
		j.stopMetrics()
		j.cleanup.Stop()
		return nil, fmt.Errorf("failed to start the scheduler: %w", err)
	}

	var watchdogCtx context.Context
	watchdogCtx, j.stopWatchdog = context.WithCancel(context.Background())
	j.watchdogDone = make(chan struct{})
	go func() {
		defer close(j.watchdogDone)
		p.Watchdog.Run(watchdogCtx)
	}()

	return j, nil
}

// stop stops the background jobs and waits for them until ctx is done. The watchdog stops first,
// since it gets the stale monitors checked through the scheduler. The scheduler cancels its
// running checks and waits for them.
func (j *jobs) stop(ctx context.Context) error {
	j.stopWatchdog()
	errs := []error{await(ctx, "the watchdog", j.watchdogDone)}

	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		j.scheduler.Stop()
	}()
	errs = append(errs, await(ctx, "the scheduler", schedulerDone))

	errs = append(errs, await(ctx, "the cleanup cron", j.cleanup.Stop().Done()))

	j.stopRepair()
	errs = append(errs, await(ctx, "the status repair", j.repairDone))

	j.stopMetrics()
	return errors.Join(errs...)
}

// await waits for done to close, or fails when ctx is done first
func await(ctx context.Context, what string, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s didn't stop in time: %w", what, ctx.Err())
	}
}

// shutdown stops everything in order, within shutdownTimeout. The HTTP server stops taking
// requests and waits for those in flight. The websocket closes its connections. The background
// jobs stop, so nothing publishes events any more. The event bus lets the handlers of the queued
// events finish. The database closes last, once nothing uses it.
func shutdown(httpServer *http.Server, p parts, j *jobs, dbType string, logger *zap.SugaredLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	var errs []error

	if err := httpServer.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("failed to shut down the HTTP server: %w", err))
		// Drop the connections still open, so that the shutdown goes on
		_ = httpServer.Close()
	}
	logger.Info("Stopped the HTTP server")

	p.WebSocket.Close()
	logger.Info("Closed the websocket server")

	if err := j.stop(ctx); err != nil {
		errs = append(errs, err)
	}
	logger.Info("Stopped the scheduler, the watchdog and the other background jobs")

	if err := p.EventBus.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("failed to close the event bus: %w", err))
	}
	logger.Info("Closed the event bus")

	if err := closeDatabase(p.DB, dbType, logger); err != nil {
		errs = append(errs, err)
	}
	logger.Info("Closed the database")

	return errors.Join(errs...)
}

// closeDatabase closes the database. SQLite checkpoints its write-ahead log first.
func closeDatabase(db *bun.DB, dbType string, logger *zap.SugaredLogger) error {
	if dbType == "sqlite" {
		return infra.GracefulSQLiteShutdown(db, dbType, logger)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	return nil
}
