package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
