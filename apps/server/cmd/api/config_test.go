package main

import (
	"testing"

	"peekaping/internal/config"

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

func TestRetentionPeriodsDefaultTo90And30Days(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	internalCfg := cfg.ToInternalConfig()
	assert.Equal(t, 90, internalCfg.NotificationHistoryRetentionDays)
	assert.Equal(t, 30, internalCfg.TLSInfoRetentionDays)
}

func TestRetentionPeriodsAreSetByTheEnvironment(t *testing.T) {
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

func TestRetentionPeriodsBelowTheirMinimumStopStartup(t *testing.T) {
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

// Validate is what stops startup for a config that didn't come through LoadAndValidate's loader
func TestValidateRefusesRetentionPeriodsBelowTheirMinimum(t *testing.T) {
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

func TestTelemetrySettingsAreLoadedFromEnv(t *testing.T) {
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

// AC1: the api refuses to start on MongoDB storage, which was removed, and points at the page that
// explains the move. The DB_TYPE validator ignores case, so the refusal does too.
func TestLoadAndValidate_RefusesMongoStorage(t *testing.T) {
	for _, dbType := range []string{"mongo", "mongodb", "MongoDB"} {
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
	for _, dbType := range []string{"mongo", "mongodb", "MongoDB"} {
		t.Run(dbType, func(t *testing.T) {
			assert.ErrorIs(t, Validate(&Config{DBType: dbType}), config.ErrMongoStorageRemoved)
		})
	}
}
