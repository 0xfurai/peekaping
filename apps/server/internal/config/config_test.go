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
