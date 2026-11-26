package metrics

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"peekaping/internal/httpx"

	"go.uber.org/zap"
)

// Listener is the port that METRICS_PORT names, bound. A nil *Listener is a metrics port that isn't
// set: Close does nothing.
type Listener struct {
	listener net.Listener
}

// Listen binds port on all interfaces. It returns nil when port is empty. A port that can't be
// bound is an error, so that startup stops. The caller binds it before it opens anything else, and
// closes it on every way out; a Server built from it closes it too, so a second Close is harmless.
func Listen(port string) (*Listener, error) {
	if port == "" {
		return nil, nil
	}
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on METRICS_PORT %s: %w", port, err)
	}
	return &Listener{listener: listener}, nil
}

// Close closes the port
func (l *Listener) Close() error {
	if l == nil {
		return nil
	}
	return l.listener.Close()
}

// Server serves the collector's /metrics alone, on a port of its own (METRICS_PORT), so that the
// API port can stay off the public internet's /metrics. A nil *Server is a metrics port that isn't
// set: its methods do nothing.
type Server struct {
	http     *http.Server
	listener net.Listener
}

// NewServer returns a server for the collector's handler on the bound port, which Serve starts. It
// returns nil when l is nil.
func NewServer(l *Listener, c *Collector, timeouts httpx.Timeouts) *Server {
	if l == nil {
		return nil
	}
	return &Server{http: httpx.NewServer("", onlyMetrics(c.Handler()), timeouts), listener: l.listener}
}

// onlyMetrics answers GET /metrics with handler and any other request with 404
func onlyMetrics(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

// Addr is the address the server listens on
func (s *Server) Addr() string { return s.listener.Addr().String() }

// Serve serves in the background. The channel gets what serving ended with: http.ErrServerClosed
// once Stop has run, which nothing needs to read. It is nil, so that a select never takes it, when
// s is nil.
func (s *Server) Serve(logger *zap.SugaredLogger) <-chan error {
	if s == nil {
		return nil
	}
	served := make(chan error, 1)
	go func() {
		logger.Infow("Serving the metrics", "address", s.Addr())
		served <- s.http.Serve(s.listener)
	}()
	return served
}

// Stop stops taking requests and waits for those in flight until ctx is done, then drops the
// connections still open. It closes the listener, even if Serve never ran.
func (s *Server) Stop(ctx context.Context, logger *zap.SugaredLogger) error {
	if s == nil {
		return nil
	}
	defer func() { _ = s.listener.Close() }()
	if err := s.http.Shutdown(ctx); err != nil {
		_ = s.http.Close()
		return fmt.Errorf("failed to shut down the metrics server: %w", err)
	}
	logger.Info("Stopped the metrics server")
	return nil
}
