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

func TestAccountSMTPIsOffByDefaultAndLoadedFromTheEnvironment(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.False(t, cfg.ToInternalConfig().AccountEmailsEnabled())
	assert.Equal(t, 587, cfg.ToInternalConfig().AccountSMTPPort)

	t.Setenv("ACCOUNT_SMTP_HOST", "smtp.example.com")
	t.Setenv("ACCOUNT_SMTP_PORT", "465")
	t.Setenv("ACCOUNT_SMTP_USER", "mailer")
	t.Setenv("ACCOUNT_SMTP_PASSWORD", "secret")
	t.Setenv("ACCOUNT_SMTP_FROM", "no-reply@example.com")
	t.Setenv("ACCOUNT_SMTP_TLS", "tls")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	internal := cfg.ToInternalConfig()
	assert.True(t, internal.AccountEmailsEnabled())
	assert.Equal(t, "smtp.example.com", internal.AccountSMTPHost)
	assert.Equal(t, 465, internal.AccountSMTPPort)
	assert.Equal(t, "mailer", internal.AccountSMTPUser)
	assert.Equal(t, "secret", internal.AccountSMTPPassword)
	assert.Equal(t, "no-reply@example.com", internal.AccountSMTPFrom)
	assert.Equal(t, "tls", internal.AccountSMTPTLS)
}

func TestAccountSMTPHostWithoutFromStopsStartup(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("ACCOUNT_SMTP_HOST", "smtp.example.com")

	_, err := LoadAndValidate(t.TempDir())

	assert.ErrorContains(t, err, "ACCOUNT_SMTP_FROM is required when ACCOUNT_SMTP_HOST is set")
}

func TestSignupModeDefaultsToInviteAndOpenIsLoadedFromTheEnvironment(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, "invite", cfg.ToInternalConfig().SignupMode)
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
	assert.Equal(t, "site", internal.SignupTurnstileSiteKey)
	assert.Equal(t, "secret", internal.SignupTurnstileSecret)
}

func TestSignupModeThatIsNotInviteOrOpenStopsStartup(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("SIGNUP_MODE", "closed")

	_, err := LoadAndValidate(t.TempDir())

	assert.ErrorContains(t, err, `SIGNUP_MODE must be invite or open, got "closed"`)
}

func TestOpenSignupWithoutASenderStopsStartup(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("SIGNUP_MODE", "open")

	_, err := LoadAndValidate(t.TempDir())

	assert.ErrorContains(t, err, "SIGNUP_MODE=open needs a sender for account emails")
}

func TestOneTurnstileKeyWithoutTheOtherStopsStartup(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("SIGNUP_TURNSTILE_SITE_KEY", "site")

	_, err := LoadAndValidate(t.TempDir())

	assert.ErrorContains(t, err, "SIGNUP_TURNSTILE_SITE_KEY and SIGNUP_TURNSTILE_SECRET must be set together")
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
