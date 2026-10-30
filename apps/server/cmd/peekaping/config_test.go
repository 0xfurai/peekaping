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

func TestLoadAndValidate_AccountSMTPIsLoadedFromTheEnvironment(t *testing.T) {
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("ACCOUNT_SMTP_HOST", "smtp.example.com")
	t.Setenv("ACCOUNT_SMTP_FROM", "no-reply@example.com")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	internal := cfg.ToInternalConfig()
	assert.True(t, internal.AccountEmailsEnabled())
	assert.Equal(t, 587, internal.AccountSMTPPort)
	assert.Equal(t, "starttls", internal.AccountSMTPTLS)
	assert.Equal(t, "no-reply@example.com", internal.AccountSMTPFrom)
}

func TestLoadAndValidate_AccountSMTPHostWithoutFromStopsStartup(t *testing.T) {
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("ACCOUNT_SMTP_HOST", "smtp.example.com")

	_, err := LoadAndValidate(t.TempDir())

	assert.ErrorContains(t, err, "ACCOUNT_SMTP_FROM is required when ACCOUNT_SMTP_HOST is set")
}

func TestLoadAndValidate_SignupModeDefaultsToInviteAndOpenIsLoadedFromTheEnvironment(t *testing.T) {
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.False(t, cfg.ToInternalConfig().SignupOpen())

	t.Setenv("SIGNUP_MODE", "open")
	t.Setenv("ACCOUNT_SMTP_HOST", "smtp.example.com")
	t.Setenv("ACCOUNT_SMTP_FROM", "no-reply@example.com")
	t.Setenv("SIGNUP_TURNSTILE_SITE_KEY", "site")
	t.Setenv("SIGNUP_TURNSTILE_SECRET", "secret")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	internal := cfg.ToInternalConfig()
	assert.True(t, internal.SignupOpen())
	assert.True(t, internal.SignupTurnstileEnabled())
}

func TestLoadAndValidate_SignupSettingsThatCanNotWorkStopStartup(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"an unknown mode", map[string]string{"SIGNUP_MODE": "closed"}, `SIGNUP_MODE must be invite or open, got "closed"`},
		{"open without a sender", map[string]string{"SIGNUP_MODE": "open"}, "SIGNUP_MODE=open needs a sender for account emails"},
		{"one Turnstile key", map[string]string{"SIGNUP_TURNSTILE_SECRET": "secret"}, "SIGNUP_TURNSTILE_SITE_KEY and SIGNUP_TURNSTILE_SECRET must be set together"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			unsetRedis(t)
			t.Setenv("DB_TYPE", "sqlite")
			t.Setenv("DB_NAME", "peekaping.db")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			_, err := LoadAndValidate(t.TempDir())

			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestDisabledMonitorTypesAreEmptyByDefaultAndLoadedFromTheEnvironment(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, cfg.ToInternalConfig().DisabledMonitorTypeList())

	t.Setenv("DISABLED_MONITOR_TYPES", " docker , snmp ")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, []string{"docker", "snmp"}, cfg.ToInternalConfig().DisabledMonitorTypeList())
}

func TestAnUnknownDisabledMonitorTypeStopsStartupAndListsTheKnownTypes(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("DISABLED_MONITOR_TYPES", "docker,dockr")

	_, err := LoadAndValidate(t.TempDir())

	assert.ErrorContains(t, err, `DISABLED_MONITOR_TYPES has an unknown monitor type "dockr": the known types are`)
	assert.ErrorContains(t, err, "docker")
	assert.ErrorContains(t, err, "http")
}

func TestBlockPrivateTargetsIsOffByDefaultAndLoadedFromTheEnvironment(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("BLOCK_PRIVATE_TARGETS", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.False(t, cfg.ToInternalConfig().BlockPrivateTargets)

	t.Setenv("BLOCK_PRIVATE_TARGETS", "true")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.True(t, cfg.ToInternalConfig().BlockPrivateTargets)
}

func TestAPIRateLimitsAreOffByDefaultAndLoadedFromTheEnvironment(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("API_RATE_LIMIT_PER_WORKSPACE", "")
	t.Setenv("API_RATE_LIMIT_PER_API_KEY", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Zero(t, cfg.ToInternalConfig().APIRateLimitPerWorkspace)
	assert.Zero(t, cfg.ToInternalConfig().APIRateLimitPerAPIKey)

	t.Setenv("API_RATE_LIMIT_PER_WORKSPACE", "600")
	t.Setenv("API_RATE_LIMIT_PER_API_KEY", "120")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, 600, cfg.ToInternalConfig().APIRateLimitPerWorkspace)
	assert.Equal(t, 120, cfg.ToInternalConfig().APIRateLimitPerAPIKey)
}

func TestANegativeAPIRateLimitStopsStartup(t *testing.T) {
	for _, name := range []string{"API_RATE_LIMIT_PER_WORKSPACE", "API_RATE_LIMIT_PER_API_KEY"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("DB_TYPE", "sqlite")
			t.Setenv("DB_NAME", "peekaping.db")
			t.Setenv(name, "-1")

			_, err := LoadAndValidate(t.TempDir())

			assert.ErrorContains(t, err, "must be at least 0")
		})
	}
}

func TestChecksPerHostPerMinuteIsOffByDefaultAndLoadedFromTheEnvironment(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("CHECKS_PER_HOST_PER_MINUTE", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Zero(t, cfg.ToInternalConfig().ChecksPerHostPerMinute)

	t.Setenv("CHECKS_PER_HOST_PER_MINUTE", "120")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, 120, cfg.ToInternalConfig().ChecksPerHostPerMinute)
}

func TestANegativeChecksPerHostPerMinuteStopsStartup(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("CHECKS_PER_HOST_PER_MINUTE", "-1")

	_, err := LoadAndValidate(t.TempDir())

	assert.ErrorContains(t, err, "must be at least 0")
}

func TestMinCheckIntervalIsTwentyByDefaultAndLoadedFromTheEnvironment(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("MIN_CHECK_INTERVAL", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, 20, cfg.ToInternalConfig().MinCheckInterval)

	t.Setenv("MIN_CHECK_INTERVAL", "60")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, 60, cfg.ToInternalConfig().MinCheckInterval)
}

func TestAMinCheckIntervalBelowTwentyStopsStartup(t *testing.T) {
	for _, value := range []string{"19", "1", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DB_TYPE", "sqlite")
			t.Setenv("DB_NAME", "peekaping.db")
			t.Setenv("MIN_CHECK_INTERVAL", value)

			_, err := LoadAndValidate(t.TempDir())

			assert.ErrorContains(t, err, "must be at least 20")
		})
	}
}

func TestSupportContactIsEmptyByDefaultAndLoadedFromTheEnvironment(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("SUPPORT_CONTACT", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, cfg.ToInternalConfig().SupportContact)

	t.Setenv("SUPPORT_CONTACT", "support@example.com")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, "support@example.com", cfg.ToInternalConfig().SupportContact)
}

func TestLoadAndValidate_TheInternalConfigKnowsWhichVariablesTheEnvironmentSet(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("CLIENT_URL", "https://peek.example")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	internal := cfg.ToInternalConfig()
	assert.True(t, internal.FromEnvironment("CLIENT_URL"))
	assert.False(t, internal.FromEnvironment("SIGNUP_MODE"))
}

func TestMetricsPortIsUnsetByDefaultAndLoadedFromTheEnvironment(t *testing.T) {
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, cfg.ToInternalConfig().MetricsPort)

	t.Setenv("METRICS_ENABLED", "true")
	t.Setenv("METRICS_PORT", "9464")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, "9464", cfg.ToInternalConfig().MetricsPort)
}

func TestMetricsPortThatCantBeServedStopsStartup(t *testing.T) {
	tests := []struct {
		name, enabled, port, serverPort, want string
	}{
		{name: "not a number", enabled: "true", port: "prometheus", want: "METRICS_PORT must be a port from 1 to 65535"},
		{name: "above 65535", enabled: "true", port: "65536", want: "METRICS_PORT must be a port from 1 to 65535"},
		{name: "zero", enabled: "true", port: "0", want: "METRICS_PORT must be a port from 1 to 65535"},
		{name: "the same as SERVER_PORT", enabled: "true", port: "8034", want: "METRICS_PORT must differ from SERVER_PORT"},
		{name: "the same as a set SERVER_PORT", enabled: "true", port: "9000", serverPort: "9000", want: "METRICS_PORT must differ from SERVER_PORT"},
		{name: "metrics off", enabled: "", port: "9464", want: "METRICS_PORT needs METRICS_ENABLED=true"},
		{name: "metrics explicitly off", enabled: "false", port: "9464", want: "METRICS_PORT needs METRICS_ENABLED=true"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			unsetRedis(t)
			t.Setenv("DB_TYPE", "sqlite")
			t.Setenv("DB_NAME", "peekaping.db")
			t.Setenv("METRICS_ENABLED", tc.enabled)
			t.Setenv("METRICS_PORT", tc.port)
			if tc.serverPort != "" {
				t.Setenv("SERVER_PORT", tc.serverPort)
			}

			_, err := LoadAndValidate(t.TempDir())

			assert.ErrorContains(t, err, tc.want)
		})
	}
}
