// Package shutdown holds the tools every Peekaping binary stops with: the signal handling that
// starts a shutdown, the time its steps are given, the waits that keep a step within its time,
// and the close of the database that comes last. cmd/peekaping and the cluster binaries share
// them so that all five stop the same way (ADR 0002).
package shutdown

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"peekaping/internal/infra"

	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// Timeouts bound the steps of a shutdown. Each step has a share of its own, so that a step
// that runs late doesn't take the time of the steps after it: the background jobs get theirs
// however long the HTTP server took.
type Timeouts struct {
	// HTTP is how long the HTTP server waits for the requests in flight
	HTTP time.Duration
	// Jobs is how long the background jobs have to stop, and the HTTP handlers still running
	// once HTTP is up have to return
	Jobs time.Duration
	// EventBus is how long the event bus has to handle the events queued
	EventBus time.Duration
}

// Total is the longest the shutdown takes, from the signal
func (t Timeouts) Total() time.Duration { return t.HTTP + t.Jobs + t.EventBus }

// NotifySignals returns a context that the first SIGTERM or SIGINT cancels, which starts the
// shutdown. A second one doesn't wait for the shutdown to finish: it is logged and exit is
// called with 1. stop stops listening for the signals.
func NotifySignals(logger *zap.SugaredLogger, exit func(code int)) (ctx context.Context, stop func()) {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		select {
		case <-signals:
			cancel()
		case <-stopped:
			return
		}
		select {
		case sig := <-signals:
			logger.Errorw("Second shutdown signal received, exiting without finishing the shutdown", "signal", sig.String())
			_ = logger.Sync()
			exit(1)
		case <-stopped:
		}
	}()

	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			signal.Stop(signals)
			close(stopped)
			cancel()
		})
	}
}

// Await waits for done to close, or fails when ctx is done first. what names the step in the
// error. done closed counts even once ctx is done.
func Await(ctx context.Context, what string, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s didn't stop in time: %w", what, ctx.Err())
	}
}

// CloseDatabase closes the database. SQLite checkpoints its write-ahead log first.
func CloseDatabase(db *bun.DB, dbType string, logger *zap.SugaredLogger) error {
	if dbType == "sqlite" {
		return infra.GracefulSQLiteShutdown(db, dbType, logger)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	return nil
}
