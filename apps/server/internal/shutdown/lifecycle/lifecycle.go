// Package lifecycle holds what a binary needs to start its shutdown and to wait for its steps,
// and nothing that stores or queues: the signal handling and the bounded wait. Package shutdown
// builds the rest of the stop on them, and the remote probe, which has no database, stops with
// these alone.
package lifecycle

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"go.uber.org/zap"
)

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
