package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"peekaping/internal/shutdown/lifecycle"
	"peekaping/internal/version"

	"go.uber.org/zap"
)

// runtime is the probe itself, as this package drives it: the session with the server, the
// scheduler that runs the checks, and the buffer that sends their results back.
type runtime interface {
	// Run keeps the probe running until ctx ends, through outages of the server. An error means
	// it can't go on.
	Run(ctx context.Context) error
	// Ready reports whether the probe has its first assignment. It stays true while the server is
	// unreachable, since the probe goes on checking.
	Ready() bool
	// Shutdown stops the timers, lets the running checks finish, flushes the results still held
	// and says bye to the server, all before ctx ends. It is called once ctx given to Run is done,
	// while Run may still be returning.
	Shutdown(ctx context.Context) error
}

// newRuntimeFunc builds the runtime from the configuration. It starts nothing: Run does.
type newRuntimeFunc func(cfg *Config, logger *zap.SugaredLogger) (runtime, error)

// shutdownTimeout is how long a shutdown has to finish, Kubernetes' default grace period less 5
// seconds. The running checks get nearly all of it, since a check cut short is lost.
// A test may shorten it.
var shutdownTimeout = 25 * time.Second

// readyHeaderTimeout bounds how long the ready endpoint waits for a request's headers
const readyHeaderTimeout = 5 * time.Second

// run starts the probe and keeps it running until ctx is done, or until it fails to start. Then
// it shuts the runtime down within shutdownTimeout, and stops serving /ready. It returns nil
// when everything stopped in time.
//
// /ready listens before the runtime starts, so that it answers 503 from the first moment. If the
// runtime's Run fails, the error is logged and the process stays up until it is told to stop:
// the ready endpoint keeps answering, and the shutdown runs as for any other signal.
func run(ctx context.Context, cfg *Config, logger *zap.SugaredLogger, build newRuntimeFunc) error {
	logger.Infow("Starting the probe", "version", version.Version, "server", redacted(cfg.URL))

	listener, err := net.Listen("tcp", cfg.ReadyAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on PROBE_READY_ADDR %s: %w", cfg.ReadyAddr, err)
	}
	rt, err := build(cfg, logger)
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("failed to build the probe: %w", err)
	}

	server := &http.Server{Handler: readyHandler(rt), ReadHeaderTimeout: readyHeaderTimeout}
	go func() {
		if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			logger.Errorw("The ready endpoint stopped serving", "error", err)
		}
	}()
	logger.Infow("Serving the ready endpoint", "addr", listener.Addr().String())

	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		// Run returns ctx's error when it stops because of the signal, which is no failure
		if err := rt.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Errorw("The probe's run ended with an error, and it keeps serving until it's told to stop", "error", err)
		}
	}()

	<-ctx.Done()
	logger.Info("Shutdown signal received, shutting down")

	// The signal has ended ctx, and the shutdown has a deadline of its own
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	var failures []error
	if err := rt.Shutdown(shutdownCtx); err != nil {
		logger.Errorw("Failed to shut the probe down", "error", err)
		failures = append(failures, fmt.Errorf("failed to shut the probe down: %w", err))
	}
	if err := lifecycle.Await(shutdownCtx, "the probe's run", runDone); err != nil {
		logger.Errorw("The probe's run didn't stop", "error", err)
		failures = append(failures, err)
	}
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
	}

	if err := errors.Join(failures...); err != nil {
		return err
	}
	logger.Info("Probe shut down")
	return nil
}

// readyHandler serves GET /ready: 200 once the runtime is ready, 503 until then
func readyHandler(rt runtime) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if !rt.Ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("waiting for the first assignment\n"))
			return
		}
		_, _ = w.Write([]byte("ready\n"))
	})
	return mux
}

// redacted is rawURL with the password, if it has one, hidden
func redacted(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "(invalid)"
	}
	return u.Redacted()
}
