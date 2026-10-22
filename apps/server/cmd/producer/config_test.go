package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_ProbeOfflineAfterDefault(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_OFFLINE_AFTER", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, 3*time.Minute, cfg.ToInternalConfig().ProbeOfflineAfter)
}

func TestLoadConfig_ProbeOfflineAfterIsLoadedFromEnv(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_OFFLINE_AFTER", "30s")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, cfg.ToInternalConfig().ProbeOfflineAfter, "30 seconds is the shortest there is")

	t.Setenv("PROBE_OFFLINE_AFTER", "10m")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, 10*time.Minute, cfg.ToInternalConfig().ProbeOfflineAfter)
}

func TestLoadConfig_ProbeOfflineAfterTooShort(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_OFFLINE_AFTER", "29s")

	_, err := LoadAndValidate(t.TempDir())

	require.Error(t, err)
	assert.ErrorContains(t, err, "PROBE_OFFLINE_AFTER must be at least 30s", "the sweep runs every 30 seconds, so a shorter limit would only flap")
}

func TestLoadConfig_ProbeOfflineAfterNegative(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_OFFLINE_AFTER", "-5m")

	_, err := LoadAndValidate(t.TempDir())

	assert.ErrorContains(t, err, "PROBE_OFFLINE_AFTER must be at least 30s")
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
