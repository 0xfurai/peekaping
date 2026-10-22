package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsAreEnabledOnlyByMetricsEnabled(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.False(t, cfg.ToInternalConfig().MetricsEnabled)

	t.Setenv("METRICS_ENABLED", "true")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.True(t, cfg.ToInternalConfig().MetricsEnabled)
}

func TestLogLevelDefaultsToInfo(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("LOG_LEVEL", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, "info", cfg.ToInternalConfig().LogLevel)
}

func TestRedisUsernameIsLoadedFromEnv(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("REDIS_USERNAME", "peekaping")
	t.Setenv("REDIS_PASSWORD", "secret")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	internalCfg := cfg.ToInternalConfig()
	assert.Equal(t, "peekaping", internalCfg.RedisUsername)
	assert.Equal(t, "secret", internalCfg.RedisPassword)
}
