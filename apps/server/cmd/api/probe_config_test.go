package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_ProbeBootstrapTokenIsEmptyByDefault(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_BOOTSTRAP_TOKEN", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Empty(t, cfg.ToInternalConfig().ProbeBootstrapToken)
}

func TestLoadConfig_ProbeBootstrapTokenIsLoadedFromEnv(t *testing.T) {
	token := strings.Repeat("k", 32)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_BOOTSTRAP_TOKEN", token)

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, token, cfg.ToInternalConfig().ProbeBootstrapToken)
}

func TestLoadConfig_ProbeBootstrapTokenUnder32CharactersStopsStartup(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_BOOTSTRAP_TOKEN", strings.Repeat("k", 31))

	_, err := LoadAndValidate(t.TempDir())

	require.Error(t, err)
	assert.ErrorContains(t, err, "PROBE_BOOTSTRAP_TOKEN must be at least 32 characters")
	assert.NotContains(t, err.Error(), strings.Repeat("k", 31), "the message doesn't repeat the token")
}

// The sweep that sets silent probes offline runs on the api replica that holds the leader lock, so
// the api reads PROBE_OFFLINE_AFTER
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
