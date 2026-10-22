package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorReportingSettingsAreLoadedFromEnv(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("ERROR_REPORTING", "")
	t.Setenv("ERROR_REPORTING_DSN", "")
	t.Setenv("DO_NOT_TRACK", "")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	internalCfg := cfg.ToInternalConfig()
	assert.False(t, internalCfg.ErrorReportingForcedOff())
	assert.Empty(t, internalCfg.ErrorReportingDSN, "the DSN built into the binary is used")

	t.Setenv("ERROR_REPORTING", "off")
	t.Setenv("ERROR_REPORTING_DSN", "https://key@errors.example.com/7")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	internalCfg = cfg.ToInternalConfig()
	assert.True(t, internalCfg.ErrorReportingForcedOff())
	assert.Equal(t, "https://key@errors.example.com/7", internalCfg.ErrorReportingDSN)

	t.Setenv("ERROR_REPORTING", "")
	t.Setenv("DO_NOT_TRACK", "1")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.True(t, cfg.ToInternalConfig().ErrorReportingForcedOff())
}
