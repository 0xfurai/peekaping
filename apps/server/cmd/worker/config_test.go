package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogLevelDefaultsToInfo(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, "info", cfg.ToInternalConfig().LogLevel)
}

func TestRedisUsernameIsLoadedFromEnv(t *testing.T) {
	t.Setenv("REDIS_USERNAME", "peekaping")
	t.Setenv("REDIS_PASSWORD", "secret")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	internalCfg := cfg.ToInternalConfig()
	assert.Equal(t, "peekaping", internalCfg.RedisUsername)
	assert.Equal(t, "secret", internalCfg.RedisPassword)
}
