package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrustedProxiesTrustNobodyByDefault(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Empty(t, cfg.ToInternalConfig().TrustedProxies)
}

func TestTrustedProxiesAreLoadedFromEnv(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("TRUSTED_PROXIES", "127.0.0.1,10.0.0.0/8")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1,10.0.0.0/8", cfg.ToInternalConfig().TrustedProxies)
}

func TestInvalidTrustedProxiesStopStartup(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8,my-proxy")

	_, err := LoadAndValidate(t.TempDir())

	require.Error(t, err)
	assert.ErrorContains(t, err, `invalid TRUSTED_PROXIES entry "my-proxy"`)
}
