// Command peekaping runs Peekaping in single mode (ADR 0002): the API and the websocket, the
// scheduler and its check pool, the ingest and the stale-monitor watchdog, in one process with
// no Redis. Cluster mode runs cmd/api and cmd/probe instead. With WEB_DIR set, it serves the web
// app on the API's port too.
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
	"time"
	_ "time/tzdata" // embed the zone database so TZ works in images without tzdata

	"peekaping/internal"
	"peekaping/internal/app"
	"peekaping/internal/config"
	"peekaping/internal/httpx"
	"peekaping/internal/limits"
	"peekaping/internal/mailer"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/cleanup"
	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/error_reporting"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/metrics"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/scheduler"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/telemetry"
	"peekaping/internal/modules/websocket"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/reporting"
	graceful "peekaping/internal/shutdown" // aliased: this package has a shutdown function
	"peekaping/internal/shutdown/lifecycle"
	"peekaping/internal/validation"
	"peekaping/internal/version"
	"peekaping/internal/web"

	"github.com/robfig/cron/v3"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// errorReportingFlush is how long the shutdown waits for the error reports still queued
const errorReportingFlush = 2 * time.Second

// shutdownTimeouts are peekaping's, 30 seconds in all. A test may shorten them.
var shutdownTimeouts = graceful.Timeouts{HTTP: 15 * time.Second, Jobs: 10 * time.Second, EventBus: 5 * time.Second}

// telemetrySchedule is when the usage ping sender checks whether a ping is due. A test may shorten
// it.
var telemetrySchedule = telemetry.DefaultSchedule

// serverTimeouts are how long the HTTP server waits on a client. A test may shorten them.
var serverTimeouts = httpx.DefaultTimeouts()

func main() {
	// A panic that ends the process is reported before it does, unless the admin opted out. Reports
	// start only once startJobs has applied the stored choice (ErrorReporting.Apply), so a panic
	// before that is not reported.
	defer reporting.ReportCrash("main")

	validation.RegisterCustomValidators()

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

	ctx, stop := lifecycle.NotifySignals(logger, os.Exit)
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
	// ProbeSweep sets the probes whose instances went quiet offline
	ProbeSweep *probe.Sweep
	// Domains verifies the custom domains by DNS when VERIFY_CUSTOM_DOMAINS is on
	Domains  domain_status_page.Service
	EventBus events.EventBus
	Mailer   *mailer.Mailer
	DB       *bun.DB

	Config               *config.Config
	Settings             setting.Service
	Probes               probe.Service
	ProbeWaker           *protocol.Waker
	Heartbeats           heartbeat.Service
	Workspaces           workspace.Repository
	WorkspaceService     workspace.Service
	WorkspaceSettings    setting.WorkspaceService
	Limits               limits.Source
	Stats                stats.Service
	NotificationHistory  notification_sent_history.Service
	TLSInfo              monitor_tls_info.Service
	AccountTokens        auth.AccountRepository
	CleanupLock          cleanup.RunLock
	Monitors             monitor.Service
	NotificationListener *notification_channel.NotificationEventListener
	MonitorListener      *monitor.MonitorEventListener
	Metrics              *metrics.Collector
	UsagePing            *telemetry.Sender
	ErrorReporting       error_reporting.Service
	TrustedProxies       *httpx.TrustedProxies
}

// run starts Peekaping and serves the API on the listener until ctx is done. With METRICS_PORT set
// it binds that port too, and serves /metrics there alone. Then it stops everything in order,
// within shutdownTimeouts: the HTTP server, the metrics server, the websocket, the background
// jobs, the event bus and the database. It returns nil when everything stopped in time. It closes
// the listener and the metrics port.
func run(ctx context.Context, cfg *Config, logger *zap.SugaredLogger, listener net.Listener) error {
	// The HTTP server closes the listener once it serves, and a failure before leaves it open
	defer func() { _ = listener.Close() }()

	// Bound before the container builds anything, so that a port that is taken stops startup with
	// nothing to close but the listeners. It serves once the jobs have started, like the HTTP
	// server, and the deferred Close covers every way out before the shutdown stops its server.
	metricsListener, err := metrics.Listen(cfg.MetricsPort)
	if err != nil {
		return err
	}
	defer func() { _ = metricsListener.Close() }()

	logger.Infow("Starting Peekaping in single mode", "version", version.Version)
	warnAboutIgnoredSettings(cfg, logger)

	container, err := newContainer(cfg.ToInternalConfig(), logger)
	if err != nil {
		return err
	}
	// The reports still queued go out as the process ends
	defer reporting.Flush(errorReportingFlush)
	p, err := resolveParts(container)
	if err != nil {
		return err
	}

	handler, err := apiHandler(cfg, p, logger)
	if err != nil {
		return err
	}

	// With METRICS_PORT set, /metrics is served on that port alone: the router and the gateway
	// leave it out
	metricsServer := metrics.NewServer(metricsListener, p.Metrics, serverTimeouts)

	jobs, err := startJobs(ctx, p, cfg, logger)
	if err != nil {
		return err
	}

	// Counted, the gateway's handlers included, so that the database closes only once every
	// handler has returned: the HTTP server's Close drops the connections still open, but doesn't
	// wait for their handlers
	handlers := graceful.NewRunningHandlers(handler)
	httpServer := httpx.NewServer("", handlers, serverTimeouts)
	// The websocket server closes as soon as the HTTP server's Shutdown stops taking connections.
	// Closing its sessions answers their open long polls, which Shutdown waits for like any
	// request in flight: left open, they'd hold it up until the clients' next ping, 25 seconds
	// away. They are answered while their handlers still wait on them, which engine.io handles
	// safely, unlike a long poll whose connection was already dropped (see websocket_test).
	websocketClosed := make(chan struct{})
	httpServer.RegisterOnShutdown(func() {
		defer close(websocketClosed)
		p.WebSocket.Close()
	})
	// A probe's assignment poll is held for up to 25 seconds, and Shutdown waits for it like any
	// request in flight: answered now, it doesn't hold the shutdown up, or make the container's
	// stop grace period run out
	httpServer.RegisterOnShutdown(p.ProbeWaker.Close)
	serveErr := serve(ctx, httpServer, listener, metricsServer, cfg.WebDir, logger)

	if err := errors.Join(serveErr, shutdown(httpServer, metricsServer, handlers, websocketClosed, p, jobs, cfg.DBType, logger)); err != nil {
		return err
	}
	logger.Info("Peekaping stopped")
	return nil
}

// newContainer registers peekaping's dependencies, with error reporting set up but still off
func newContainer(internalCfg *config.Config, logger *zap.SugaredLogger) (*dig.Container, error) {
	container := dig.New()
	if err := app.RegisterSingle(container, internalCfg); err != nil {
		return nil, fmt.Errorf("failed to register dependencies: %w", err)
	}
	// Every module logs through the caller's logger
	if err := container.Decorate(func() *zap.SugaredLogger { return logger }); err != nil {
		return nil, fmt.Errorf("failed to decorate the logger: %w", err)
	}
	// Error reports stay off until startJobs applies the admin's choice
	if err := app.SetupErrorReporting(container, internalCfg); err != nil {
		return nil, fmt.Errorf("failed to set up error reporting: %w", err)
	}
	return container, nil
}

// resolveParts builds everything run starts and stops
func resolveParts(container *dig.Container) (parts, error) {
	var p parts
	if err := container.Invoke(func(resolved parts) { p = resolved }); err != nil {
		return parts{}, fmt.Errorf("failed to build peekaping: %w", err)
	}
	return p, nil
}

// warnAboutIgnoredSettings says which settings single mode ignores: the Redis settings, and the
// probe bootstrap token
func warnAboutIgnoredSettings(cfg *Config, logger *zap.SugaredLogger) {
	if ignored := cfg.RedisSettings(); len(ignored) > 0 {
		logger.Warnw("Single mode uses no Redis, ignoring the Redis settings", "settings", ignored)
	}

	if cfg.ProbeBootstrapToken != "" {
		// Not stored: this process checks the local probe's monitors, and the probe protocol
		// refuses the local probe's token. Never the token itself.
		logger.Warn("PROBE_BOOTSTRAP_TOKEN is set, but single mode doesn't use it: the peekaping process checks the local probe's monitors and refuses a probe instance that uses the local probe's token. Give peekaping-probe a remote probe's token.")
	}
}

// apiHandler is the handler that serves every request. The API router, or with WEB_DIR set the
// gateway, which sends the router the API's paths and serves the web app on the others. The
// gateway answers some requests itself, so it gets the request ID and the request log too. The
// router's own pass a request that comes through the gateway.
func apiHandler(cfg *Config, p parts, logger *zap.SugaredLogger) (http.Handler, error) {
	if cfg.WebDir == "" {
		return p.Server.Router, nil
	}
	gateway, err := web.NewGateway(web.Config{Dir: cfg.WebDir, APIURL: cfg.APIURL, Metrics: cfg.MetricsEnabled && cfg.MetricsPort == ""}, p.Server.Router, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to serve the web app from WEB_DIR: %w", err)
	}
	return httpx.Chain(gateway, httpx.RequestID, httpx.Logger(logger, p.TrustedProxies)), nil
}

// serve serves the API on the listener, and the metrics on their own port when there is one, until
// ctx is done. It returns what failed, if serving failed: the shutdown begins then.
func serve(ctx context.Context, httpServer *http.Server, listener net.Listener, metricsServer *metrics.Server, webDir string, logger *zap.SugaredLogger) error {
	served := make(chan error, 1)
	go func() {
		logger.Infow("Serving the API", "address", listener.Addr().String(), "web_dir", webDir)
		served <- httpServer.Serve(listener)
	}()
	metricsServed := metricsServer.Serve(logger)

	// Serve returns http.ErrServerClosed once the shutdown has begun, when nothing reads it any
	// more: here it can only be a failure
	select {
	case <-ctx.Done():
		logger.Info("Shutdown signal received, shutting down")
		return nil
	case err := <-served:
		failure := fmt.Errorf("HTTP server failed: %w", err)
		logger.Errorw("HTTP server failed, shutting down", "error", failure)
		return failure
	case err := <-metricsServed:
		failure := fmt.Errorf("metrics server failed: %w", err)
		logger.Errorw("Metrics server failed, shutting down", "error", failure)
		return failure
	}
}

// jobs are the background jobs run starts, and how to stop them
type jobs struct {
	scheduler     *scheduler.Scheduler
	stopWatchdog  context.CancelFunc
	watchdogDone  chan struct{}
	stopSweep     context.CancelFunc
	sweepDone     chan struct{}
	stopDomains   context.CancelFunc
	domainsDone   chan struct{}
	cleanup       *cron.Cron
	stopRepair    context.CancelFunc
	repairDone    chan struct{}
	stopMetrics   context.CancelFunc
	metricsDone   <-chan struct{}
	stopUsagePing context.CancelFunc
	usagePingDone <-chan struct{}
}

// startJobs starts what cmd/api starts beside the server (the cleanup cron, the settings, the
// listeners, the status repair, the metrics collector and the usage ping), the stats aggregation,
// and the scheduler, its watchdog and the probe sweep that the api's leader runs in cluster mode.
// Each runs once: this process is the only one, and so always the leader.
func startJobs(ctx context.Context, p parts, cfg *Config, logger *zap.SugaredLogger) (*jobs, error) {
	if err := p.Settings.InitializeSettings(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize JWT settings: %w", err)
	}

	// The admin's choice from before the restart. A failure leaves error reports off.
	if err := p.ErrorReporting.Apply(ctx); err != nil {
		logger.Warnw("Failed to apply the error reporting choice, leaving error reports off", "error", err)
	}

	j := &jobs{scheduler: p.Scheduler}
	retention := cleanup.Retention{
		NotificationHistoryDays: cfg.NotificationHistoryRetentionDays,
		TLSInfoDays:             cfg.TLSInfoRetentionDays,
	}
	var err error
	j.cleanup, err = cleanup.StartCleanupCron(p.Heartbeats, p.Workspaces, p.WorkspaceSettings, p.Limits, p.Stats, p.NotificationHistory, p.TLSInfo, p.Probes, p.AccountTokens, p.WorkspaceService, p.CleanupLock, retention, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to start the hourly cleanup: %w", err)
	}

	p.NotificationListener.Subscribe(p.EventBus)
	p.MonitorListener.Subscribe(p.EventBus)
	// The probes' waiting assignment polls answer when a monitor or one of a probe's instances changes
	p.ProbeWaker.Subscribe()
	if impl, ok := p.Stats.(*stats.ServiceImpl); ok {
		impl.RegisterEventHandlers(p.EventBus)
	}

	// Repair monitors whose stored status differs from their latest heartbeat. It runs beside
	// startup and only logs, so a failure never keeps the API from serving.
	var repairCtx context.Context
	repairCtx, j.stopRepair = context.WithCancel(context.Background())
	j.repairDone = make(chan struct{})
	reporting.Go("repair", func() {
		defer close(j.repairDone)
		repaired, err := p.Monitors.RepairStoredStatusesAnyWorkspace(repairCtx)
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

	var metricsCtx context.Context
	metricsCtx, j.stopMetrics = context.WithCancel(context.Background())
	if cfg.MetricsEnabled {
		j.metricsDone = p.Metrics.Start(metricsCtx, p.EventBus)
	} else {
		done := make(chan struct{})
		close(done)
		j.metricsDone = done
	}

	// The scheduler and the watchdog run until the shutdown stops them, not until ctx is done,
	// so that they stop after the HTTP server
	if err := p.Scheduler.Start(context.Background()); err != nil {
		j.stopRepair()
		j.stopMetrics()
		j.cleanup.Stop()
		return nil, fmt.Errorf("failed to start the scheduler: %w", err)
	}

	var watchdogCtx context.Context
	watchdogCtx, j.stopWatchdog = context.WithCancel(context.Background())
	j.watchdogDone = make(chan struct{})
	reporting.Go("watchdog", func() {
		defer close(j.watchdogDone)
		p.Watchdog.Run(watchdogCtx)
	})

	// The sweep that sets silent probes offline. This process is the only one, so it runs the
	// sweep once, as the leader api replica does in cluster mode.
	var sweepCtx context.Context
	sweepCtx, j.stopSweep = context.WithCancel(context.Background())
	j.sweepDone = make(chan struct{})
	reporting.Go("probe-sweep", func() {
		defer close(j.sweepDone)
		p.ProbeSweep.Run(sweepCtx)
	})

	// The check of custom domains runs once, as the leader api replica runs it in cluster mode. It
	// returns at once while VERIFY_CUSTOM_DOMAINS is off.
	j.stopDomains, j.domainsDone = startDomainChecks(p.Domains)

	// The usage ping is sent a few minutes after the start, unless an admin opted out or the
	// environment forces it off, and a failed one is dropped. It runs once: this process is the
	// only one, so there is no claim to win.
	var usagePingCtx context.Context
	usagePingCtx, j.stopUsagePing = context.WithCancel(context.Background())
	j.usagePingDone = p.UsagePing.Start(usagePingCtx, telemetrySchedule)

	return j, nil
}

// startDomainChecks starts the check of custom domains, and returns how to stop it and its done channel
func startDomainChecks(domains domain_status_page.Service) (context.CancelFunc, chan struct{}) {
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	reporting.Go("custom-domain-checks", func() {
		defer close(done)
		domains.Run(ctx)
	})
	return stop, done
}

// stop stops the background jobs and waits for them until ctx is done. The usage ping stops
// first, since it is the least needed. The watchdog stops before the scheduler, since it gets the
// stale monitors checked through the scheduler. The probe sweep stops with it. The scheduler
// cancels its running checks and waits for them.
func (j *jobs) stop(ctx context.Context) error {
	j.stopUsagePing()
	errs := []error{lifecycle.Await(ctx, "the usage ping", j.usagePingDone)}

	j.stopWatchdog()
	errs = append(errs, lifecycle.Await(ctx, "the watchdog", j.watchdogDone))

	j.stopSweep()
	errs = append(errs, lifecycle.Await(ctx, "the probe sweep", j.sweepDone))

	j.stopDomains()
	errs = append(errs, lifecycle.Await(ctx, "the custom domain checks", j.domainsDone))

	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		j.scheduler.Stop()
	}()
	errs = append(errs, lifecycle.Await(ctx, "the scheduler", schedulerDone))

	errs = append(errs, lifecycle.Await(ctx, "the cleanup cron", j.cleanup.Stop().Done()))

	j.stopRepair()
	errs = append(errs, lifecycle.Await(ctx, "the status repair", j.repairDone))

	j.stopMetrics()
	errs = append(errs, lifecycle.Await(ctx, "the metrics collector", j.metricsDone))
	return errors.Join(errs...)
}

// shutdown stops everything in order, each step within its share of shutdownTimeouts. The HTTP
// server stops taking requests and waits for those in flight; once its share is up, it drops
// their connections. The websocket closes its sessions, from the HTTP server's Shutdown
// (websocketClosed is closed once it has). The metrics server stops right after, in the same
// share. The background jobs stop, so nothing publishes events any more, and the HTTP handlers
// the HTTP server left running return, in the jobs' share. The event bus lets the handlers of the
// queued events finish. The database closes last, and only once nothing uses it.
func shutdown(httpServer *http.Server, metricsServer *metrics.Server, handlers *graceful.RunningHandlers, websocketClosed <-chan struct{}, p parts, j *jobs, dbType string, logger *zap.SugaredLogger) error {
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
	// It reads only the collector's memory, so it needs nothing that stops below, and stops in the
	// HTTP server's share
	if err := metricsServer.Stop(httpCtx, logger); err != nil {
		errs = append(errs, err)
	}

	<-websocketClosed
	logger.Info("Closed the websocket server")

	// What uses the database and still runs once its share is up
	var running []string

	jobsCtx, cancelJobs := context.WithTimeout(context.Background(), shutdownTimeouts.Jobs)
	defer cancelJobs()
	if err := j.stop(jobsCtx); err != nil {
		errs = append(errs, err)
		running = append(running, "background jobs")
	} else {
		logger.Info("Stopped the scheduler, the watchdog and the other background jobs")
	}
	// The HTTP handlers the HTTP server left running return in the jobs' share too, before the
	// event bus closes, so that what they publish reaches it
	if err := lifecycle.Await(jobsCtx, "the HTTP handlers", handlers.Done()); err != nil {
		errs = append(errs, err)
		running = append(running, "HTTP handlers")
	}

	// No handler can start an account email any more. Let the ones being sent go out before the
	// database closes: a send reads and writes it. It is bound by graceful.MailerDrain, which the
	// shares of shutdownTimeouts don't include: it waits only while a send is running.
	graceful.DrainMailer(p.Mailer, graceful.MailerDrain, logger)

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
	if err := graceful.CloseDatabase(p.DB, dbType, logger); err != nil {
		errs = append(errs, err)
	}
	logger.Info("Closed the database")

	return errors.Join(errs...)
}
