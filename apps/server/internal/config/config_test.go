package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadTestConfig(t *testing.T, env map[string]string) (Config, error) {
	t.Helper()
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	for k, v := range env {
		t.Setenv(k, v)
	}
	return LoadConfig[Config](t.TempDir())
}

func TestLoadConfig_CheckConcurrencyDefaultsTo128(t *testing.T) {
	cfg, err := loadTestConfig(t, nil)

	require.NoError(t, err)
	assert.Equal(t, 128, cfg.CheckConcurrency)
}

func TestLoadConfig_CheckConcurrencyIsSetByTheEnvironment(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"CHECK_CONCURRENCY": "16"})

	require.NoError(t, err)
	assert.Equal(t, 16, cfg.CheckConcurrency)
}

func TestLoadConfig_CheckConcurrencyBelowOneIsRejected(t *testing.T) {
	_, err := loadTestConfig(t, map[string]string{"CHECK_CONCURRENCY": "-1"})

	assert.ErrorContains(t, err, "CheckConcurrency")
}

func TestLoadConfig_RetentionPeriodsDefaultTo90And30Days(t *testing.T) {
	cfg, err := loadTestConfig(t, nil)

	require.NoError(t, err)
	assert.Equal(t, 90, cfg.NotificationHistoryRetentionDays)
	assert.Equal(t, 30, cfg.TLSInfoRetentionDays)
}

func TestLoadConfig_RetentionPeriodsAreSetByTheEnvironment(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{
		"NOTIFICATION_HISTORY_RETENTION_DAYS": "45",
		"TLS_INFO_RETENTION_DAYS":             "7",
	})

	require.NoError(t, err)
	assert.Equal(t, 45, cfg.NotificationHistoryRetentionDays)
	assert.Equal(t, 7, cfg.TLSInfoRetentionDays)
}

func TestLoadConfig_RetentionPeriodsAtTheirMinimumAreAccepted(t *testing.T) {
	// 30 days keeps the history past the default certificate expiry notification at 21 days
	cfg, err := loadTestConfig(t, map[string]string{
		"NOTIFICATION_HISTORY_RETENTION_DAYS": "30",
		"TLS_INFO_RETENTION_DAYS":             "1",
	})

	require.NoError(t, err)
	assert.Equal(t, 30, cfg.NotificationHistoryRetentionDays)
	assert.Equal(t, 1, cfg.TLSInfoRetentionDays)
}

func TestLoadConfig_RetentionPeriodsBelowTheirMinimumAreRejected(t *testing.T) {
	for name, tc := range map[string]struct{ env, value, want string }{
		"notification history, 29": {"NOTIFICATION_HISTORY_RETENTION_DAYS", "29", "NotificationHistoryRetentionDays must be at least 30"},
		"notification history, -1": {"NOTIFICATION_HISTORY_RETENTION_DAYS", "-1", "NotificationHistoryRetentionDays must be at least 30"},
		"TLS info, -1":             {"TLS_INFO_RETENTION_DAYS", "-1", "TLSInfoRetentionDays must be at least 1"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadTestConfig(t, map[string]string{tc.env: tc.value})

			require.Error(t, err)
			assert.ErrorContains(t, err, tc.want)
			assert.NotContains(t, err.Error(), "characters", "a number of days isn't a length")
		})
	}
}

func TestLoadConfig_TelemetryEndpointDefaultsToPeekapingsWorker(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"TELEMETRY_URL": ""})

	require.NoError(t, err)
	assert.Equal(t, "https://telemetry.peekaping.com/v1/ping", cfg.TelemetryURL)
}

func TestLoadConfig_TelemetryEndpointIsSetByTheEnvironment(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"TELEMETRY_URL": "http://localhost:8787/v1/ping"})

	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8787/v1/ping", cfg.TelemetryURL)
}

func TestLoadConfig_TelemetryKillSwitchComesFromTheEnvironment(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"TELEMETRY": "off"})

	require.NoError(t, err)
	assert.Equal(t, "off", cfg.Telemetry)
}

func TestLoadConfig_DoNotTrackAloneLeavesBothFeaturesOn(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"DO_NOT_TRACK": "1", "TELEMETRY": "", "ERROR_REPORTING": ""})

	require.NoError(t, err)
	assert.False(t, cfg.TelemetryForcedOff(), "DO_NOT_TRACK=1 no longer switches off the usage ping")
	assert.False(t, cfg.ErrorReportingForcedOff(), "DO_NOT_TRACK=1 no longer switches off error reports")
}

func TestConfig_TelemetryForcedOff(t *testing.T) {
	for _, tc := range []struct {
		name      string
		telemetry string
		want      bool
	}{
		{"nothing set", "", false},
		{"TELEMETRY=on", "on", false},
		{"TELEMETRY=true", "true", false},
		{"TELEMETRY=off", "off", true},
		{"TELEMETRY=OFF", "OFF", true},
		{"TELEMETRY=false", "false", true},
		{"TELEMETRY=False", "False", true},
		{"TELEMETRY=0", "0", true},
		{"TELEMETRY=no", "no", true},
		{"TELEMETRY=No", "No", true},
		{"TELEMETRY=off with spaces", " off ", true},
		{"TELEMETRY=1 is not an off value", "1", false},
		{"TELEMETRY=anything else", "maybe", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Telemetry: tc.telemetry}

			assert.Equal(t, tc.want, cfg.TelemetryForcedOff())
		})
	}
}

func TestLoadConfig_ErrorReportingSettingsComeFromTheEnvironment(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"ERROR_REPORTING": "off", "ERROR_REPORTING_DSN": "https://key@errors.example.com/7"})

	require.NoError(t, err)
	assert.Equal(t, "off", cfg.ErrorReporting)
	assert.Equal(t, "https://key@errors.example.com/7", cfg.ErrorReportingDSN)
}

func TestLoadConfig_ErrorReportingSettingsAreEmptyByDefault(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"ERROR_REPORTING": "", "ERROR_REPORTING_DSN": ""})

	require.NoError(t, err)
	assert.Empty(t, cfg.ErrorReporting)
	assert.Empty(t, cfg.ErrorReportingDSN, "the DSN built into the binary is used")
	assert.False(t, cfg.ErrorReportingForcedOff())
}

func TestConfig_ErrorReportingForcedOff(t *testing.T) {
	for _, tc := range []struct {
		name           string
		errorReporting string
		want           bool
	}{
		{"nothing set", "", false},
		{"ERROR_REPORTING=on", "on", false},
		{"ERROR_REPORTING=true", "true", false},
		{"ERROR_REPORTING=off", "off", true},
		{"ERROR_REPORTING=OFF", "OFF", true},
		{"ERROR_REPORTING=false", "false", true},
		{"ERROR_REPORTING=0", "0", true},
		{"ERROR_REPORTING=no", "no", true},
		{"ERROR_REPORTING=No", "No", true},
		{"ERROR_REPORTING=off with spaces", " off ", true},
		{"ERROR_REPORTING=1 is not an off value", "1", false},
		{"ERROR_REPORTING=anything else", "maybe", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{ErrorReporting: tc.errorReporting}

			assert.Equal(t, tc.want, cfg.ErrorReportingForcedOff())
		})
	}
}

func TestConfig_TheTwoSwitchesAreIndependent(t *testing.T) {
	assert.False(t, (&Config{Telemetry: "off"}).ErrorReportingForcedOff(), "TELEMETRY=off keeps error reports on")
	assert.False(t, (&Config{ErrorReporting: "off"}).TelemetryForcedOff(), "ERROR_REPORTING=off keeps the usage ping on")
}

func TestMaskSensitiveValue_MasksTheErrorReportingDSN(t *testing.T) {
	assert.Equal(t, "*****", maskSensitiveValue("ERROR_REPORTING_DSN", "https://key@errors.example.com/7"))
}

func TestMaskSensitiveValue_MasksTheProbeBootstrapToken(t *testing.T) {
	assert.Equal(t, "*****", maskSensitiveValue("PROBE_BOOTSTRAP_TOKEN", "0123456789abcdef0123456789abcdef"))
}

// AC1: MongoDB storage was removed, and every binary says so, not that DB_TYPE is invalid. The
// db_type validator ignores case, so the refusal does too.
func TestLoadConfig_RefusesMongoStorage(t *testing.T) {
	for _, dbType := range []string{"mongo", "mongodb", "MongoDB", "MONGO"} {
		t.Run(dbType, func(t *testing.T) {
			_, err := loadTestConfig(t, map[string]string{
				"DB_TYPE": dbType, "DB_NAME": "peekaping", "DB_HOST": "mongo", "DB_PORT": "27017", "DB_USER": "peekaping", "DB_PASS": "secret",
			})

			require.ErrorIs(t, err, ErrMongoStorageRemoved)
			assert.Contains(t, err.Error(), "DB_TYPE="+dbType+": MongoDB storage was removed. "+
				`Move this install's data to SQLite or PostgreSQL with "bun db import-mongo", then set DB_TYPE to sqlite or postgres. `+
				"See https://docs.peekaping.com/self-hosting/docker-with-mongo#move-to-sqlite-or-postgresql")
			assert.NotContains(t, err.Error(), "must be one of", "not the validator's message")
		})
	}
}

// "bun db migrate" loads only the database settings
func TestLoadConfig_RefusesMongoStorageInTheDatabaseSettings(t *testing.T) {
	for _, dbType := range []string{"mongo", "mongodb", "MongoDB"} {
		t.Run(dbType, func(t *testing.T) {
			t.Setenv("DB_TYPE", dbType)
			t.Setenv("DB_NAME", "peekaping")

			_, err := LoadConfig[DBConfig](t.TempDir())

			require.ErrorIs(t, err, ErrMongoStorageRemoved)
			assert.ErrorContains(t, err, "DB_TYPE="+dbType+": MongoDB storage was removed")
		})
	}
}

func TestLoadConfig_AcceptsTheSQLStorageBackends(t *testing.T) {
	for _, dbType := range []string{"sqlite", "postgres", "postgresql", "mysql", "SQLite"} {
		t.Run(dbType, func(t *testing.T) {
			_, err := loadTestConfig(t, map[string]string{"DB_TYPE": dbType})

			assert.NoError(t, err)
		})
	}
}

func TestLoadConfig_AnUnknownDBTypeStillGetsTheValidatorsMessage(t *testing.T) {
	_, err := loadTestConfig(t, map[string]string{"DB_TYPE": "oracle"})

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrMongoStorageRemoved)
	assert.ErrorContains(t, err, "DBType must be one of: postgres, postgresql, mysql, sqlite")
	assert.NotContains(t, err.Error(), "mongo", "MongoDB isn't a choice any more")
}

func TestRefuseMongoStorage(t *testing.T) {
	for _, dbType := range []string{"mongo", "mongodb", "Mongo", "MONGODB", " mongo "} {
		assert.ErrorIs(t, RefuseMongoStorage(dbType), ErrMongoStorageRemoved, dbType)
	}
	for _, dbType := range []string{"", "sqlite", "postgres", "postgresql", "mysql", "oracle"} {
		assert.NoError(t, RefuseMongoStorage(dbType), dbType)
	}
}
