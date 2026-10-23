package main

import (
	"os"
	"path/filepath"
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

func TestLoadAndValidate_ReadsTheTelemetrySettings(t *testing.T) {
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("TELEMETRY", "")
	t.Setenv("TELEMETRY_URL", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	internalCfg := cfg.ToInternalConfig()
	assert.False(t, internalCfg.TelemetryForcedOff())
	assert.Equal(t, "https://telemetry.peekaping.com/v1/ping", internalCfg.TelemetryURL)

	t.Setenv("TELEMETRY", "off")
	t.Setenv("TELEMETRY_URL", "http://localhost:8787/v1/ping")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	internalCfg = cfg.ToInternalConfig()
	assert.True(t, internalCfg.TelemetryForcedOff())
	assert.Equal(t, "http://localhost:8787/v1/ping", internalCfg.TelemetryURL)
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
	// The DB_TYPE validator ignores case, so the refusal does too
	for _, dbType := range []string{"mongo", "mongodb", "MONGO", "MongoDB"} {
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

func TestLoadAndValidate_ServesNoWebAppByDefault(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("WEB_DIR", "")
	t.Setenv("API_URL", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Empty(t, cfg.WebDir, "the API alone, as today")
	assert.Empty(t, cfg.APIURL, "the web app reaches the API on its own origin")
}

func TestLoadAndValidate_KeepsWebDirAndAPIURLFromTheEnvironment(t *testing.T) {
	webDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<!doctype html>"), 0o644))
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("WEB_DIR", webDir)
	t.Setenv("API_URL", "https://api.example.com")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, webDir, cfg.WebDir)
	assert.Equal(t, "https://api.example.com", cfg.APIURL)
}

func TestLoadAndValidate_RefusesAWebDirWithoutAWebBuild(t *testing.T) {
	for name, webDir := range map[string]string{
		"missing":          filepath.Join(t.TempDir(), "missing"),
		"no index.html in": t.TempDir(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("DB_TYPE", "sqlite")
			t.Setenv("DB_NAME", "peekaping.db")
			t.Setenv("WEB_DIR", webDir)

			_, err := LoadAndValidate(t.TempDir())

			require.Error(t, err)
			assert.Contains(t, err.Error(), "WEB_DIR="+webDir)
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
