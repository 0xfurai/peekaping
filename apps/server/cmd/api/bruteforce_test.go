package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccountLockoutDefaults(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	internalCfg := cfg.ToInternalConfig()
	assert.Equal(t, 100, internalCfg.BruteforceAccountMaxAttempts)
	assert.Equal(t, 10*time.Minute, internalCfg.BruteforceAccountWindow)
	assert.Equal(t, time.Minute, internalCfg.BruteforceAccountLockout)
}

func TestAccountLockoutIsSetByTheEnvironment(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("BRUTEFORCE_ACCOUNT_MAX_ATTEMPTS", "30")
	t.Setenv("BRUTEFORCE_ACCOUNT_WINDOW", "1h")
	t.Setenv("BRUTEFORCE_ACCOUNT_LOCKOUT", "5m")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	internalCfg := cfg.ToInternalConfig()
	assert.Equal(t, 30, internalCfg.BruteforceAccountMaxAttempts)
	assert.Equal(t, time.Hour, internalCfg.BruteforceAccountWindow)
	assert.Equal(t, 5*time.Minute, internalCfg.BruteforceAccountLockout)
}

func TestAccountLockoutSettingsMustBePositive(t *testing.T) {
	for name, value := range map[string]string{
		"BRUTEFORCE_ACCOUNT_MAX_ATTEMPTS": "-1",
		"BRUTEFORCE_ACCOUNT_WINDOW":       "-1m",
		"BRUTEFORCE_ACCOUNT_LOCKOUT":      "-1m",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("DB_TYPE", "sqlite")
			t.Setenv("DB_NAME", "peekaping.db")
			t.Setenv(name, value)

			_, err := LoadAndValidate(t.TempDir())

			require.Error(t, err)
			assert.ErrorContains(t, err, name)
		})
	}
}
