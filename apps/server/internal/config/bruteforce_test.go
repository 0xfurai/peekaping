package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_BruteforceDefaults(t *testing.T) {
	cfg, err := loadTestConfig(t, nil)

	require.NoError(t, err)
	assert.Equal(t, 20, cfg.BruteforceMaxAttempts)
	assert.Equal(t, time.Minute, cfg.BruteforceWindow)
	assert.Equal(t, time.Minute, cfg.BruteforceLockout)
	assert.Equal(t, 100, cfg.BruteforceAccountMaxAttempts)
	assert.Equal(t, 10*time.Minute, cfg.BruteforceAccountWindow)
	assert.Equal(t, time.Minute, cfg.BruteforceAccountLockout)
	assert.Empty(t, cfg.TrustedProxies, "no proxy is trusted by default")
}

func TestLoadConfig_BruteforceAccountSettingsAreSetByTheEnvironment(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{
		"BRUTEFORCE_ACCOUNT_MAX_ATTEMPTS": "30",
		"BRUTEFORCE_ACCOUNT_WINDOW":       "1h",
		"BRUTEFORCE_ACCOUNT_LOCKOUT":      "5m",
		"TRUSTED_PROXIES":                 "127.0.0.1,10.0.0.0/8",
	})

	require.NoError(t, err)
	assert.Equal(t, 30, cfg.BruteforceAccountMaxAttempts)
	assert.Equal(t, time.Hour, cfg.BruteforceAccountWindow)
	assert.Equal(t, 5*time.Minute, cfg.BruteforceAccountLockout)
	assert.Equal(t, "127.0.0.1,10.0.0.0/8", cfg.TrustedProxies)
}
