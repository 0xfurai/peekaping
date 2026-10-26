package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_ProbeBootstrapTokenIsEmptyByDefault(t *testing.T) {
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_BOOTSTRAP_TOKEN", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Empty(t, cfg.ProbeBootstrapToken)
}

func TestLoadConfig_ProbeBootstrapTokenIsReadSoThatPeekapingCanSayItIgnoresIt(t *testing.T) {
	token := strings.Repeat("k", 32)
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_BOOTSTRAP_TOKEN", token)

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, token, cfg.ProbeBootstrapToken)
}

// Single mode doesn't use the token, so a short one is no reason to stop
func TestLoadConfig_ProbeBootstrapTokenUnder32CharactersDoesNotStopStartup(t *testing.T) {
	unsetRedis(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("PROBE_BOOTSTRAP_TOKEN", "short")

	cfg, err := LoadAndValidate(t.TempDir())

	require.NoError(t, err)
	assert.Equal(t, "short", cfg.ProbeBootstrapToken)
}
