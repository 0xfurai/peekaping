//go:build !slim

package executor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"peekaping/internal/modules/shared"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// startDockerDaemon serves the two requests of a container inspect: the version ping and the
// inspect of container "web", which it answers with the given status and body. It returns the
// daemon's tcp:// address.
func startDockerDaemon(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.43")
			_, _ = w.Write([]byte("OK"))
		case strings.HasSuffix(r.URL.Path, "/containers/web/json"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return "tcp://" + strings.TrimPrefix(srv.URL, "http://")
}

func dockerTCPMonitor(daemon string) *Monitor {
	return &Monitor{
		ID:      "docker",
		Type:    "docker",
		Name:    "web",
		Timeout: 5,
		Config:  fmt.Sprintf(`{"container_id": "web", "connection_type": "tcp", "docker_daemon": %q}`, daemon),
	}
}

func TestDockerExecutor_Execute_ContainerStates(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantStatus  shared.MonitorStatus
		wantMessage string
	}{
		{name: "running without a health check", body: `{"Id": "web", "State": {"Running": true, "Status": "running"}}`, wantStatus: shared.MonitorStatusUp, wantMessage: "running"},
		{name: "running and healthy", body: `{"Id": "web", "State": {"Running": true, "Status": "running", "Health": {"Status": "healthy"}}}`, wantStatus: shared.MonitorStatusUp, wantMessage: "healthy"},
		{name: "running and still starting", body: `{"Id": "web", "State": {"Running": true, "Status": "running", "Health": {"Status": "starting"}}}`, wantStatus: shared.MonitorStatusPending, wantMessage: "starting"},
		{name: "running and unhealthy", body: `{"Id": "web", "State": {"Running": true, "Status": "running", "Health": {"Status": "unhealthy"}}}`, wantStatus: shared.MonitorStatusDown, wantMessage: "container is unhealthy: unhealthy"},
		{name: "running with another health status", body: `{"Id": "web", "State": {"Running": true, "Status": "running", "Health": {"Status": "none"}}}`, wantStatus: shared.MonitorStatusDown, wantMessage: "container health status: none"},
		{name: "not running", body: `{"Id": "web", "State": {"Running": false, "Status": "exited"}}`, wantStatus: shared.MonitorStatusDown, wantMessage: "container state is exited"},
		{name: "not running but healthy", body: `{"Id": "web", "State": {"Running": false, "Status": "paused", "Health": {"Status": "healthy"}}}`, wantStatus: shared.MonitorStatusDown, wantMessage: "container state is paused"},
		{name: "no state", body: `{"Id": "web", "State": null}`, wantStatus: shared.MonitorStatusDown, wantMessage: "container state is nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			daemon := startDockerDaemon(t, http.StatusOK, tt.body)

			result := NewDockerExecutor(zap.NewNop().Sugar()).Execute(context.Background(), dockerTCPMonitor(daemon), nil)

			assert.Equal(t, tt.wantStatus, result.Status, result.Message)
			assert.Equal(t, tt.wantMessage, result.Message)
			assert.False(t, result.StartTime.IsZero())
			assert.False(t, result.EndTime.Before(result.StartTime))
		})
	}
}

func TestDockerExecutor_Execute_InspectErrors(t *testing.T) {
	tests := []struct {
		name        string
		daemonError string
		wantMessage string
	}{
		{name: "plain error", daemonError: "boom", wantMessage: "container inspect error: Error response from daemon: boom"},
		{
			name:        "legacy common name",
			daemonError: "x509: certificate relies on legacy Common Name field, use SANs instead",
			wantMessage: "container inspect error: Error response from daemon: x509: certificate relies on legacy Common Name field, use SANs instead\n\nHint: The Docker daemon's TLS certificate uses legacy Common Name instead of Subject Alternative Names. Try setting 'Verify TLS' to false, or update your Docker daemon with a proper certificate that includes SANs",
		},
		{
			name:        "unknown authority",
			daemonError: "x509: certificate signed by unknown authority",
			wantMessage: "container inspect error: Error response from daemon: x509: certificate signed by unknown authority\n\nHint: The Docker daemon's certificate is not trusted. Provide a CA certificate or set 'Verify TLS' to false",
		},
		{
			name:        "failed verification",
			daemonError: "tls: failed to verify certificate",
			wantMessage: "container inspect error: Error response from daemon: tls: failed to verify certificate\n\nHint: TLS certificate verification failed. Check your certificates or set 'Verify TLS' to false for testing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			daemon := startDockerDaemon(t, http.StatusInternalServerError, fmt.Sprintf(`{"message": %q}`, tt.daemonError))

			result := NewDockerExecutor(zap.NewNop().Sugar()).Execute(context.Background(), dockerTCPMonitor(daemon), nil)

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Equal(t, tt.wantMessage, result.Message)
		})
	}
}

func TestDockerExecutor_Execute_TLSConfigurationError(t *testing.T) {
	monitor := &Monitor{
		ID:      "docker",
		Type:    "docker",
		Name:    "web",
		Timeout: 5,
		Config:  `{"container_id": "web", "connection_type": "tcp", "docker_daemon": "tcp://127.0.0.1:2376", "tls_enabled": true, "tls_cert": "only a certificate"}`,
	}

	result := NewDockerExecutor(zap.NewNop().Sugar()).Execute(context.Background(), monitor, nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Equal(t, "TLS configuration error: both certificate and key must be provided for client authentication", result.Message)
}

func TestDockerExecutor_Execute_UnknownConnectionType(t *testing.T) {
	monitor := &Monitor{
		ID:      "docker",
		Type:    "docker",
		Name:    "web",
		Timeout: 5,
		Config:  `{"container_id": "web", "connection_type": "ssh", "docker_daemon": "ssh://host"}`,
	}

	result := NewDockerExecutor(zap.NewNop().Sugar()).Execute(context.Background(), monitor, nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Equal(t, "unknown docker connection type: ssh", result.Message)
}

func TestDockerExecutor_Execute_InvalidConfigMessage(t *testing.T) {
	result := NewDockerExecutor(zap.NewNop().Sugar()).Execute(context.Background(), &Monitor{Config: `{"nope": 1}`}, nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	require.True(t, strings.HasPrefix(result.Message, "invalid config: failed to parse config:"), result.Message)
}

func TestDockerExecutor_Execute_SocketDaemonUnreachable(t *testing.T) {
	monitor := &Monitor{
		ID:      "docker",
		Type:    "docker",
		Name:    "web",
		Timeout: 5,
		Config:  `{"container_id": "web", "connection_type": "socket", "docker_daemon": "/nonexistent/docker.sock"}`,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result := NewDockerExecutor(zap.NewNop().Sugar()).Execute(ctx, monitor, nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.True(t, strings.HasPrefix(result.Message, "container inspect error: "), result.Message)
}
