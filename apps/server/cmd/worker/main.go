// Command worker runs the worker of Peekaping in cluster mode (ADR 0002), beside cmd/api,
// cmd/producer and cmd/ingester. It runs the checks it takes from the healthcheck queue and
// enqueues their results for the ingester. It has no database.
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
	"peekaping/internal/modules/worker"
	"peekaping/internal/reporting"
	"peekaping/internal/reporting/redissync"
	graceful "peekaping/internal/shutdown" // aliased: this package has a shutdown function
	"peekaping/internal/version"

	"github.com/redis/go-redis/v9"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// shutdownTimeouts are the worker's, 30 seconds in all, Kubernetes' default grace period. The
// running checks get nearly all of it, since a check cut short is run again from the start by
// another worker. The event bus has no handler here and closes at once. A test may shorten them.
var shutdownTimeouts = graceful.Timeouts{Jobs: 25 * time.Second, EventBus: 5 * time.Second}

// errorReportingFlush is how long the shutdown waits for the error reports still queued
const errorReportingFlush = 2 * time.Second

func main() {
	// A panic that ends the process is reported before it does, if the admin opted in
	defer reporting.ReportCrash("main")

	cfg, err := LoadAndValidate("../..")
	if err != nil {
		log.Fatalf("Failed to load and validate Worker config: %v", err)
	}

	os.Setenv("TZ", cfg.Timezone)

	logger, err := internal.ProvideLogger(cfg.ToInternalConfig())
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	ctx, stop := graceful.NotifySignals(logger, os.Exit)
	defer stop()

	if err := run(ctx, cfg, logger); err != nil {
		logger.Fatalw("The worker stopped with an error", "error", err)
	}
}

// parts are what run starts and stops
type parts struct {
	dig.In

	Worker   *worker.Worker
	EventBus events.EventBus
	Redis    *redis.Client // the api copies the error reporting choice there
}

// run starts the worker and keeps it running until ctx is done, or until it fails to start.
// Then it stops everything that started, in order, within shutdownTimeouts: the worker, which
// lets its running checks finish and enqueue their results, and the event bus. It returns nil
// when the worker ran until ctx was done and everything stopped in time.
func run(ctx context.Context, cfg *Config, logger *zap.SugaredLogger) error {
	logger.Infow("Starting the worker in cluster mode", "version", version.Version)
	p, err := build(cfg, logger)
	if err != nil {
		return err
	}

	// Error reports stay off until the api's copy of the admin's choice is read from Redis, now
	// and every minute: the worker has no database to read the setting from. The reports still queued go out as the process ends.
	defer reporting.Flush(errorReportingFlush)
	errorReportingCtx, stopErrorReporting := context.WithCancel(context.Background())
	defer stopErrorReporting()
	refresher := redissync.NewRefresher(p.Redis, redissync.DefaultEvery)
	reporting.Go("error-reporting", func() { refresher.Run(errorReportingCtx) })
	return serve(ctx, p, logger)
}

// build registers the worker's dependencies, every module logging through the caller's logger,
// and gets what run starts and stops from them
func build(cfg *Config, logger *zap.SugaredLogger) (parts, error) {
	internalCfg := cfg.ToInternalConfig()
	// asynq gives up on the checks still running before the jobs' share is over
	internalCfg.QueueShutdownTimeout = shutdownTimeouts.QueueShutdown()
	container := dig.New()
	if err := app.RegisterClusterWorker(container, internalCfg); err != nil {
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
		return parts{}, fmt.Errorf("failed to build the worker: %w", err)
	}
	return p, nil
}

// serve starts the worker, and stops it and what it uses once ctx is done or the start fails
func serve(ctx context.Context, p parts, logger *zap.SugaredLogger) error {
	var failure error
	if err := p.Worker.Start(); err != nil {
		failure = fmt.Errorf("failed to start the worker: %w", err)
		logger.Errorw("Failed to start the worker, shutting down", "error", err)
	} else {
		logger.Info("Serving: the worker is running")
		<-ctx.Done()
		logger.Info("Shutdown signal received, shutting down")
	}

	// The worker has no database
	if err := errors.Join(failure, graceful.Stop(shutdownTimeouts, "the worker", p.Worker.Stop, p.EventBus, nil, logger)); err != nil {
		return err
	}
	logger.Info("Worker shut down")
	return nil
}
