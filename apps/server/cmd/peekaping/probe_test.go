package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// bootstrapToken is 43 characters
const bootstrapToken = "bootstrap-token-0123456789-abcdefghijklmnop"

// localProbe reads the local probe through the API
func (in *instance) localProbe(t *testing.T) (tokenPrefix string) {
	t.Helper()
	status, body := in.request(t, http.MethodGet, "/api/v1/probes/local", nil)
	return decode[struct {
		ID          string `json:"id"`
		TokenPrefix string `json:"token_prefix"`
	}](t, status, body, http.StatusOK).TokenPrefix
}

func TestRun_StoresTheProbeBootstrapTokenOnTheLocalProbe(t *testing.T) {
	cfg := loadConfig(t, map[string]string{"PROBE_BOOTSTRAP_TOKEN": bootstrapToken})
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)

	assert.Equal(t, "bootstra", in.localProbe(t), "the local probe has the token's prefix")

	applied := logs.FilterMessageSnippet("PROBE_BOOTSTRAP_TOKEN").All()
	require.Len(t, applied, 1)
	assert.Contains(t, applied[0].Message, "local probe")
	for _, entry := range logs.All() {
		assert.NotContains(t, entry.Message, bootstrapToken)
	}
}

func TestRun_WithoutAProbeBootstrapTokenTheLocalProbeHasNoToken(t *testing.T) {
	cfg := loadConfig(t, map[string]string{"PROBE_BOOTSTRAP_TOKEN": ""})
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)

	assert.Equal(t, "", in.localProbe(t))
	assert.Empty(t, logs.FilterMessageSnippet("PROBE_BOOTSTRAP_TOKEN").All())
}

func TestRun_ProbeBootstrapTokenIsKeptAcrossARestart(t *testing.T) {
	cfg := loadConfig(t, map[string]string{"PROBE_BOOTSTRAP_TOKEN": bootstrapToken})
	logger, logs := observedLogger()
	first := startPeekaping(t, cfg, logger, logs)
	first.registerAdmin(t)
	require.NoError(t, first.stop(t))

	// The same database, started again without the token: local keeps the one it has
	cfg.ProbeBootstrapToken = ""
	second := startPeekaping(t, cfg, zap.NewNop().Sugar(), logs)
	second.login(t)

	assert.Equal(t, "bootstra", second.localProbe(t))
}

// login logs the admin in, who registered before a restart, and sends the requests that follow
// with the access token
func (in *instance) login(t *testing.T) {
	t.Helper()
	status, body := in.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": adminEmail, "password": adminPassword,
	})
	in.accessToken = decode[struct {
		AccessToken string `json:"accessToken"`
	}](t, status, body, http.StatusOK).AccessToken
}
