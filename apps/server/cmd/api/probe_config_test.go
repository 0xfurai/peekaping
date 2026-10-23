package main

import (
	"strings"
	"testing"

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
