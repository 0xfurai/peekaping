// Command ingester runs the ingester of Peekaping in cluster mode (ADR 0002), beside cmd/api,
// cmd/producer and cmd/worker. It stores the results it takes from the ingester queue as
// heartbeats, and aggregates the stats that follow from them.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"
	_ "time/tzdata" // embed the zone database so TZ works in images without tzdata

	"peekaping/internal"
	"peekaping/internal/app"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/stats"
	"peekaping/internal/reporting"
	"peekaping/internal/reporting/redissync"
	graceful "peekaping/internal/shutdown" // aliased: this package has a shutdown function
	"peekaping/internal/version"

	"github.com/redis/go-redis/v9"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// shutdownTimeouts are the ingester's, 30 seconds in all, Kubernetes' default grace period. An
// ingest is a few writes, so the running ones finish well within the jobs' share, which is
// longer for a database that is busy. The event bus gets the rest, since the handlers of the
// events the ingests published (the stats) write to the database too. A test may shorten them.
var shutdownTimeouts = graceful.Timeouts{Jobs: 20 * time.Second, EventBus: 10 * time.Second}

// errorReportingFlush is how long the shutdown waits for the error reports still queued
const errorReportingFlush = 2 * time.Second

func main() {
	// A panic that ends the process is reported before it does, if the admin opted in
	defer reporting.ReportCrash("main")

	cfg, err := LoadAndValidate("../..")
	if err != nil {
		log.Fatalf("Failed to load and validate Ingester config: %v", err)
	}

	os.Setenv("TZ", cfg.Timezone)

	logger, err := internal.ProvideLogger(cfg.ToInternalConfig())
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	ctx, stop := graceful.NotifySignals(logger, os.Exit)
	defer stop()

	if err := run(ctx, cfg, logger); err != nil {
		logger.Fatalw("The ingester stopped with an error", "error", err)
	}
}

// parts are what run starts and stops
type parts struct {
	dig.In

	Ingester *ingester.Ingester
	Stats    stats.Service
	EventBus events.EventBus
	Database graceful.Database
	Redis    *redis.Client // the api copies the error reporting choice there
}

// run starts the ingester and keeps it running until ctx is done, or until it fails to start.
// Then it stops everything that started, in order, within shutdownTimeouts: the ingester, which
// lets its running ingests finish storing their heartbeats, the event bus and the database. It
// returns nil when the ingester ran until ctx was done and everything stopped in time.
func run(ctx context.Context, cfg *Config, logger *zap.SugaredLogger) error {
	logger.Infow("Starting the ingester in cluster mode", "version", version.Version)
	p, err := build(cfg, logger)
	if err != nil {
		return err
	}

	// Error reports stay off until the api's copy of the admin's choice is read from Redis, now
	// and every minute. The reports still queued go out as the process ends.
	defer reporting.Flush(errorReportingFlush)
	errorReportingCtx, stopErrorReporting := context.WithCancel(context.Background())
	defer stopErrorReporting()
	refresher := redissync.NewRefresher(p.Redis, redissync.DefaultEvery)
	reporting.Go("error-reporting", func() { refresher.Run(errorReportingCtx) })
	return serve(ctx, p, cfg.DBType, logger)
}

// build registers the ingester's dependencies, every module logging through the caller's logger,
// and gets what run starts and stops from them
func build(cfg *Config, logger *zap.SugaredLogger) (parts, error) {
	internalCfg := cfg.ToInternalConfig()
	// asynq gives up on the ingests still running before the jobs' share is over
	internalCfg.QueueShutdownTimeout = shutdownTimeouts.QueueShutdown()
	container := dig.New()
	if err := app.RegisterClusterIngester(container, internalCfg); err != nil {
		return parts{}, fmt.Errorf("failed to register dependencies: %w", err)
	}
	if err := container.Decorate(func() *zap.SugaredLogger { return logger }); err != nil {
		return parts{}, fmt.Errorf("failed to decorate the logger: %w", err)
	}
	if err := app.SetupErrorReporting(container, internalCfg); err != nil {
		return parts{}, fmt.Errorf("failed to set up error reporting: %w", err)
	}
	var p parts
	if err := container.Invoke(func(resolved parts) { p = resolved }); err != nil {
		return parts{}, fmt.Errorf("failed to build the ingester: %w", err)
	}
	return p, nil
}

// serve starts the ingester, and stops it and what it uses once ctx is done or the start fails
func serve(ctx context.Context, p parts, dbType string, logger *zap.SugaredLogger) error {
	if impl, ok := p.Stats.(*stats.ServiceImpl); ok {
		impl.RegisterEventHandlers(p.EventBus)
	}

	var failure error
	if err := p.Ingester.Start(); err != nil {
		failure = fmt.Errorf("failed to start the ingester: %w", err)
		logger.Errorw("Failed to start the ingester, shutting down", "error", err)
	} else {
		logger.Info("Serving: the ingester is running")
		<-ctx.Done()
		logger.Info("Shutdown signal received, shutting down")
	}

	closeDatabase := func(ctx context.Context) error { return p.Database.Close(ctx, dbType, logger) }
	if err := errors.Join(failure, graceful.Stop(shutdownTimeouts, "the ingester", p.Ingester.Stop, p.EventBus, closeDatabase, logger)); err != nil {
		return err
	}
	logger.Info("Ingester shut down")
	return nil
}
