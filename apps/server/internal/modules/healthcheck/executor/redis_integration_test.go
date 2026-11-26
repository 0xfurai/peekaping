//go:build integration

package executor

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"peekaping/internal/modules/shared"
	"peekaping/internal/testservice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRedisExecutor_Integration(t *testing.T) {
	srv := testservice.Redis(t)
	exec := NewRedisExecutor(zap.NewNop().Sugar())

	run := func(t *testing.T, password string) *Result {
		t.Helper()
		cfg, err := json.Marshal(RedisConfig{
			DatabaseConnectionString: (&url.URL{Scheme: "redis", User: url.UserPassword("", password), Host: srv.Addr}).String(),
		})
		require.NoError(t, err)
		return exec.Execute(context.Background(), &Monitor{
			ID:      "integration-redis",
			Name:    "Integration Redis",
			Type:    "redis",
			Timeout: 5,
			Config:  string(cfg),
		}, nil)
	}

	t.Run("is up with the right password", func(t *testing.T) {
		result := run(t, srv.Password)

		assert.Equal(t, shared.MonitorStatusUp, result.Status)
		assert.Contains(t, result.Message, "Redis ping successful")
	})

	t.Run("is down with a wrong password", func(t *testing.T) {
		result := run(t, "not-"+srv.Password)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "WRONGPASS")
	})
}
