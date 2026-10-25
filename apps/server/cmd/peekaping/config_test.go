package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"peekaping/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unsetRedis clears the REDIS_* variables for the test: the config ignores an empty one
func unsetRedis(t *testing.T) {
	for _, name := range []string{"REDIS_HOST", "REDIS_PORT", "REDIS_USERNAME", "REDIS_PASSWORD", "REDIS_DB"} {
		t.Setenv(name, "")
	}
}

func TestLoadAndValidate_LogLevelDefaultsToInfo(t *testing.T) {
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("LOG_LEVEL", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, "info", cfg.ToInternalConfig().LogLevel)
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

func TestLoadConfig_ProbeOfflineAfterDefault(t *testing.T) {
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_OFFLINE_AFTER", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, 3*time.Minute, cfg.ToInternalConfig().ProbeOfflineAfter)
}

func TestLoadConfig_ProbeOfflineAfterIsLoadedFromEnv(t *testing.T) {
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_OFFLINE_AFTER", "2m")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, 2*time.Minute, cfg.ToInternalConfig().ProbeOfflineAfter, "2 minutes is the shortest there is")

	t.Setenv("PROBE_OFFLINE_AFTER", "10m")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, 10*time.Minute, cfg.ToInternalConfig().ProbeOfflineAfter)
}

func TestLoadConfig_ProbeOfflineAfterTooShort(t *testing.T) {
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_OFFLINE_AFTER", "1m59s")

	_, err := LoadAndValidate(t.TempDir())

	require.Error(t, err)
	assert.ErrorContains(t, err, "PROBE_OFFLINE_AFTER must be at least 2m", "a probe that restarts gracefully must not be announced offline")
}

// The sweep forgets an instance that went unseen for 24 hours, so a longer limit would let it set a
// quiet probe online on an instance that is about to be pruned, and offline again on the next pass.
func TestLoadConfig_ProbeOfflineAfterTooLong(t *testing.T) {
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")

	t.Setenv("PROBE_OFFLINE_AFTER", "24h")
	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err, "24 hours is the longest there is")
	assert.Equal(t, 24*time.Hour, cfg.ToInternalConfig().ProbeOfflineAfter)

	t.Setenv("PROBE_OFFLINE_AFTER", "24h1s")
	_, err = LoadAndValidate(t.TempDir())
	require.Error(t, err)
	assert.ErrorContains(t, err, "PROBE_OFFLINE_AFTER must be at most 24h0m0s", "the sweep forgets an instance after that")
}

func TestLoadConfig_ProbeOfflineAfterNegative(t *testing.T) {
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_OFFLINE_AFTER", "-5m")

	_, err := LoadAndValidate(t.TempDir())

	assert.ErrorContains(t, err, "PROBE_OFFLINE_AFTER must be at least 2m")
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

// AC1: MongoDB storage was removed, so peekaping says so and points at the page that explains the move.
// The DB_TYPE validator ignores case, so the refusal does too.
func TestLoadAndValidate_RefusesMongoStorage(t *testing.T) {
	for _, dbType := range []string{"mongo", "mongodb", "MONGO", "MongoDB"} {
		t.Run(dbType, func(t *testing.T) {
			t.Setenv("DB_TYPE", dbType)
			t.Setenv("DB_NAME", "peekaping")
			t.Setenv("DB_HOST", "mongo")
			t.Setenv("DB_PORT", "27017")
			t.Setenv("DB_USER", "peekaping")
			t.Setenv("DB_PASS", "secret")

			_, err := LoadAndValidate(t.TempDir())

			require.ErrorIs(t, err, config.ErrMongoStorageRemoved)
			assert.Contains(t, err.Error(), "DB_TYPE="+dbType+": MongoDB storage was removed.")
			assert.Contains(t, err.Error(), "https://docs.peekaping.com/self-hosting/docker-with-mongo#move-to-sqlite-or-postgresql")
			assert.Contains(t, err.Error(), `"bun db import-mongo"`)
		})
	}
}

// Validate is called on its own too, with a Config that didn't come from LoadAndValidate
func TestValidate_RefusesMongoStorage(t *testing.T) {
	for _, dbType := range []string{"mongo", "mongodb", "MONGO", "MongoDB"} {
		t.Run(dbType, func(t *testing.T) {
			err := Validate(&Config{DBType: dbType})

			assert.ErrorIs(t, err, config.ErrMongoStorageRemoved)
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

func TestLoadAndValidate_RetentionPeriodsDefaultTo90And30Days(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	internalCfg := cfg.ToInternalConfig()
	assert.Equal(t, 90, internalCfg.NotificationHistoryRetentionDays)
	assert.Equal(t, 30, internalCfg.TLSInfoRetentionDays)
}

func TestLoadAndValidate_KeepsRetentionPeriodsFromTheEnvironment(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("NOTIFICATION_HISTORY_RETENTION_DAYS", "45")
	t.Setenv("TLS_INFO_RETENTION_DAYS", "7")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	internalCfg := cfg.ToInternalConfig()
	assert.Equal(t, 45, internalCfg.NotificationHistoryRetentionDays)
	assert.Equal(t, 7, internalCfg.TLSInfoRetentionDays)
}

func TestLoadAndValidate_RefusesRetentionPeriodsBelowTheirMinimum(t *testing.T) {
	for name, tc := range map[string]struct{ env, value, want string }{
		"notification history, 29": {"NOTIFICATION_HISTORY_RETENTION_DAYS", "29", "NotificationHistoryRetentionDays must be at least 30"},
		"TLS info, -1":             {"TLS_INFO_RETENTION_DAYS", "-1", "TLSInfoRetentionDays must be at least 1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("DB_TYPE", "sqlite")
			t.Setenv("DB_NAME", "peekaping.db")
			t.Setenv(tc.env, tc.value)

			_, err := LoadAndValidate(t.TempDir())

			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestValidate_RefusesRetentionPeriodsBelowTheirMinimum(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	cfg.NotificationHistoryRetentionDays = 0
	assert.ErrorContains(t, Validate(cfg), "NotificationHistoryRetentionDays")

	cfg.NotificationHistoryRetentionDays = 30
	cfg.TLSInfoRetentionDays = 0
	assert.ErrorContains(t, Validate(cfg), "TLSInfoRetentionDays")
}

// SQLITE_STARTUP_CHECK picks the check a process runs when it opens SQLite. Unset, it is the quick
// check, as before the setting existed.
func TestSQLiteStartupCheckDefaultsToQuick(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("SQLITE_STARTUP_CHECK", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, "quick", cfg.ToInternalConfig().SQLiteStartupCheck)
}

func TestSQLiteStartupCheckIsKeptFromTheEnvironment(t *testing.T) {
	for _, mode := range []string{"quick", "full", "off"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("DB_TYPE", "sqlite")
			t.Setenv("DB_NAME", "peekaping.db")
			t.Setenv("SQLITE_STARTUP_CHECK", mode)

			cfg, err := LoadAndValidate(t.TempDir())
			require.NoError(t, err)

			assert.Equal(t, mode, cfg.ToInternalConfig().SQLiteStartupCheck)
		})
	}
}

// A mistyped value stops startup: running a check the operator didn't ask for, or none, would hide
// that the setting doesn't do what they think.
func TestSQLiteStartupCheckRefusesAnUnknownValue(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("SQLITE_STARTUP_CHECK", "fast")

	_, err := LoadAndValidate(t.TempDir())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "SQLiteStartupCheck must be one of: quick full off")
}
