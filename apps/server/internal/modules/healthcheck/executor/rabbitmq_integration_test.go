//go:build integration

package executor

import (
	"context"
	"strings"
	"testing"

	"peekaping/internal/modules/shared"
	"peekaping/internal/testservice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRabbitMQExecutor_Integration(t *testing.T) {
	srv := testservice.RabbitMQ(t)
	exec := NewRabbitMQExecutor(zap.NewNop().Sugar())

	run := func(t *testing.T, password string, nodes ...string) *Result {
		t.Helper()
		return exec.Execute(context.Background(), rabbitMQMonitor(t, 5, RabbitMQConfig{
			Nodes:    nodes,
			Username: srv.Username,
			Password: password,
		}), nil)
	}

	t.Run("is up with the right credentials", func(t *testing.T) {
		result := run(t, srv.Password, srv.URL)

		assert.Equal(t, shared.MonitorStatusUp, result.Status)
		assert.Equal(t, "OK", result.Message)
	})

	t.Run("is down with a wrong password", func(t *testing.T) {
		result := run(t, "not-"+srv.Password, srv.URL)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Equal(t, "All RabbitMQ nodes failed: unexpected status code: 401", result.Message)
	})

	t.Run("sends a password with reserved characters intact", func(t *testing.T) {
		// Guards the broker's password, not the executor: if it lost these
		// characters, the case would stop testing what it names.
		require.True(t, strings.ContainsAny(srv.Password, "@:/%"), "the broker's password must hold reserved characters")

		result := run(t, srv.Password, srv.URL)

		assert.Equal(t, shared.MonitorStatusUp, result.Status)
		assert.Equal(t, "OK", result.Message)
	})

	t.Run("fails over to the second node when the first refuses", func(t *testing.T) {
		result := run(t, srv.Password, refusedNodeURL(t), srv.URL)

		assert.Equal(t, shared.MonitorStatusUp, result.Status)
		assert.Equal(t, "OK", result.Message)
	})

	t.Run("is down with the dial error when every node refuses", func(t *testing.T) {
		first, second := refusedNodeURL(t), refusedNodeURL(t)

		result := run(t, srv.Password, first, second)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		// The message carries the last node's error only.
		assert.Contains(t, result.Message, "All RabbitMQ nodes failed: request failed: ")
		assert.Contains(t, result.Message, "dial tcp "+strings.TrimPrefix(second, "http://")+": connect: connection refused")
	})

	t.Run("is down when an https node points at the plain port", func(t *testing.T) {
		result := run(t, srv.Password, strings.Replace(srv.URL, "http://", "https://", 1))

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "http: server gave HTTP response to HTTPS client")
	})
}
