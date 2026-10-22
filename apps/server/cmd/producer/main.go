// Command producer runs the producer of Peekaping in cluster mode (ADR 0002), beside cmd/api,
// cmd/worker and cmd/ingester. It schedules the checks and enqueues them for the worker while it
// is the leader, and runs the watchdog that gets the stale monitors checked again.
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
	"peekaping/internal/modules/producer"
	"peekaping/internal/reporting"
	"peekaping/internal/reporting/redissync"
	graceful "peekaping/internal/shutdown" // aliased: this package has a shutdown function
	"peekaping/internal/version"

	"github.com/redis/go-redis/v9"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// shutdownTimeouts are the producer's, 30 seconds in all, Kubernetes' default grace period. The
// producer stops scheduling and its watchdog as soon as its context is cancelled, and the claim
// in flight finishes enqueuing its check. Only then does it release the leadership, with a call
// to Redis that may take 5 seconds: the jobs' share allows for both. The event bus gets the
// rest. A test may shorten them.
var shutdownTimeouts = graceful.Timeouts{Jobs: 20 * time.Second, EventBus: 10 * time.Second}

// errorReportingFlush is how long the shutdown waits for the error reports still queued
const errorReportingFlush = 2 * time.Second

func main() {
	// A panic that ends the process is reported before it does, if the admin opted in
	defer reporting.ReportCrash("main")

	cfg, err := LoadAndValidate("../..")
	if err != nil {
		log.Fatalf("Failed to load and validate Producer config: %v", err)
	}

	os.Setenv("TZ", cfg.Timezone)

	logger, err := internal.ProvideLogger(cfg.ToInternalConfig())
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	ctx, stop := graceful.NotifySignals(logger, os.Exit)
	defer stop()

	if err := run(ctx, cfg, logger); err != nil {
		logger.Fatalw("The producer stopped with an error", "error", err)
	}
}

// parts are what run starts and stops
type parts struct {
	dig.In

	Producer *producer.Producer
	Listener *producer.EventListener
	EventBus events.EventBus
	Database graceful.Database
	Redis    *redis.Client // the api copies the error reporting choice there
}

// run starts the producer and keeps it running until ctx is done, or until it fails to start.
// Then it stops everything that started, in order, within shutdownTimeouts: the producer, with
// its leadership released, the event bus and the database. It returns nil when the producer ran
// until ctx was done and everything stopped in time.
func run(ctx context.Context, cfg *Config, logger *zap.SugaredLogger) error {
	logger.Infow("Starting the producer in cluster mode", "version", version.Version)
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

// build registers the producer's dependencies, every module logging through the caller's logger,
// and gets what run starts and stops from them
func build(cfg *Config, logger *zap.SugaredLogger) (parts, error) {
	internalCfg := cfg.ToInternalConfig()
	container := dig.New()
	if err := app.RegisterClusterProducer(container, internalCfg); err != nil {
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
		return parts{}, fmt.Errorf("failed to build the producer: %w", err)
	}
	return p, nil
}

// serve starts the producer, and stops it and what it uses once ctx is done or the start fails
func serve(ctx context.Context, p parts, dbType string, logger *zap.SugaredLogger) error {
	p.Listener.Subscribe(p.EventBus)

	// The producer runs until the shutdown stops it, not until ctx is done, so that it stops in
	// its place in the order, within its share of the time
	var failure error
	if err := p.Producer.Start(context.Background()); err != nil {
		failure = fmt.Errorf("failed to start the producer: %w", err)
		logger.Errorw("Failed to start the producer, shutting down", "error", err)
	} else {
		logger.Info("Serving: the producer is running")
		<-ctx.Done()
		logger.Info("Shutdown signal received, shutting down")
	}

	closeDatabase := func() error { return p.Database.Close(context.Background(), dbType, logger) }
	if err := errors.Join(failure, graceful.Stop(shutdownTimeouts, "the producer", p.Producer.Stop, p.EventBus, closeDatabase, logger)); err != nil {
		return err
	}
	logger.Info("Producer shut down")
	return nil
}
