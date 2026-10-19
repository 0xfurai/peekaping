package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsAreEnabledOnlyByMetricsEnabled(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.False(t, cfg.ToInternalConfig().MetricsEnabled)

	t.Setenv("METRICS_ENABLED", "true")
	cfg, err = LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	assert.True(t, cfg.ToInternalConfig().MetricsEnabled)
}
