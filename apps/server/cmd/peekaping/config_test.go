package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unsetRedis clears the REDIS_* variables for the test: the config ignores an empty one
func unsetRedis(t *testing.T) {
	for _, name := range []string{"REDIS_HOST", "REDIS_PORT", "REDIS_USERNAME", "REDIS_PASSWORD", "REDIS_DB"} {
		t.Setenv(name, "")
	}
}

func TestLoadAndValidate_NeedsNoRedis(t *testing.T) {
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Empty(t, cfg.RedisSettings())
	internalCfg := cfg.ToInternalConfig()
	assert.Empty(t, internalCfg.RedisHost, "single mode has no Redis to connect to")
	assert.Equal(t, "8034", internalCfg.Port, "SERVER_PORT keeps the API's default")
	assert.Equal(t, 128, internalCfg.CheckConcurrency)
}

func TestLoadAndValidate_ListsTheRedisSettingsItIgnores(t *testing.T) {
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("REDIS_HOST", "redis")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("REDIS_DB", "0")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, []string{"REDIS_HOST", "REDIS_PASSWORD", "REDIS_DB"}, cfg.RedisSettings(), "names only, never the values")
	assert.Empty(t, cfg.ToInternalConfig().RedisHost)
}

func TestLoadAndValidate_RefusesMongoStorage(t *testing.T) {
	for _, dbType := range []string{"mongo", "mongodb"} {
		t.Run(dbType, func(t *testing.T) {
			t.Setenv("DB_TYPE", dbType)
			t.Setenv("DB_NAME", "peekaping")
			t.Setenv("DB_HOST", "mongo")
			t.Setenv("DB_PORT", "27017")
			t.Setenv("DB_USER", "peekaping")
			t.Setenv("DB_PASS", "secret")

			_, err := LoadAndValidate(t.TempDir())

			require.Error(t, err)
			assert.Contains(t, err.Error(), "DB_TYPE="+dbType+" isn't supported by peekaping (single mode)")
			assert.Contains(t, err.Error(), "SQLite or PostgreSQL")
		})
	}
}

func TestLoadAndValidate_KeepsCheckConcurrencyFromTheEnvironment(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("CHECK_CONCURRENCY", "16")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, 16, cfg.ToInternalConfig().CheckConcurrency)
}
