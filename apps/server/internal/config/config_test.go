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
