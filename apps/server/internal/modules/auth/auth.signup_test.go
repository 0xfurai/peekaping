package auth_test

import (
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/modules/auth"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func signupWarnings(t *testing.T, cfg config.Config) []observer.LoggedEntry {
	t.Helper()
	core, logs := observer.New(zap.WarnLevel)
	auth.NewSignupGate(&cfg, nil, zap.New(core).Sugar())
	return logs.AllUntimed()
}

func TestNewSignupGate_OpenSignupWithoutTrustedProxiesWarnsAtStartup(t *testing.T) {
	for _, proxies := range []string{"", "  "} {
		warnings := signupWarnings(t, config.Config{SignupMode: config.SignupModeOpen, TrustedProxies: proxies})

		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0].Message, "TRUSTED_PROXIES")
		assert.Contains(t, warnings[0].Message, "share one limit")
	}
}

func TestNewSignupGate_NoWarningWhenProxiesAreTrustedOrSignupIsByInvite(t *testing.T) {
	assert.Empty(t, signupWarnings(t, config.Config{SignupMode: config.SignupModeOpen, TrustedProxies: "10.0.0.0/8"}))
	assert.Empty(t, signupWarnings(t, config.Config{SignupMode: config.SignupModeInvite}))
	assert.Empty(t, signupWarnings(t, config.Config{}))
}
