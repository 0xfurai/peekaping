package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_ProbeOfflineAfterDefault(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_OFFLINE_AFTER", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, 3*time.Minute, cfg.ToInternalConfig().ProbeOfflineAfter)
}

func TestLoadConfig_ProbeOfflineAfterIsLoadedFromEnv(t *testing.T) {
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
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_OFFLINE_AFTER", "-5m")

	_, err := LoadAndValidate(t.TempDir())

	assert.ErrorContains(t, err, "PROBE_OFFLINE_AFTER must be at least 2m")
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
