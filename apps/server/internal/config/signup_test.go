package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSignup(t *testing.T) {
	tests := []struct {
		name                        string
		mode, siteKey, secret, smtp string
		wantErrorContaining         string
	}{
		{name: "unset is invite", mode: ""},
		{name: "invite", mode: "invite"},
		{name: "invite needs no sender", mode: "invite", smtp: ""},
		{name: "open with a sender", mode: "open", smtp: "smtp.example.com"},
		{name: "open without a sender", mode: "open", wantErrorContaining: "SIGNUP_MODE=open needs a sender for account emails: set ACCOUNT_SMTP_HOST"},
		{name: "open with a blank sender host", mode: "open", smtp: "  ", wantErrorContaining: "SIGNUP_MODE=open needs a sender for account emails"},
		{name: "an unknown mode", mode: "closed", wantErrorContaining: `SIGNUP_MODE must be invite or open, got "closed"`},
		{name: "the mode is case sensitive", mode: "Open", smtp: "smtp.example.com", wantErrorContaining: "SIGNUP_MODE must be invite or open"},
		{name: "both Turnstile keys", mode: "open", smtp: "smtp.example.com", siteKey: "site", secret: "secret"},
		{name: "the site key alone", mode: "open", smtp: "smtp.example.com", siteKey: "site", wantErrorContaining: "SIGNUP_TURNSTILE_SITE_KEY and SIGNUP_TURNSTILE_SECRET must be set together"},
		{name: "the secret alone", mode: "invite", secret: "secret", wantErrorContaining: "SIGNUP_TURNSTILE_SITE_KEY and SIGNUP_TURNSTILE_SECRET must be set together"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSignup(tc.mode, tc.siteKey, tc.secret, tc.smtp)
			if tc.wantErrorContaining == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tc.wantErrorContaining)
		})
	}
}

func TestLoadConfig_SignupDefaultsToInvite(t *testing.T) {
	cfg, err := loadTestConfig(t, nil)

	require.NoError(t, err)
	assert.Equal(t, "invite", cfg.SignupMode)
	assert.False(t, cfg.SignupOpen())
	assert.False(t, cfg.SignupTurnstileEnabled())
}

func TestLoadConfig_SignupIsSetByTheEnvironment(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{
		"SIGNUP_MODE": "open", "SIGNUP_TURNSTILE_SITE_KEY": "site", "SIGNUP_TURNSTILE_SECRET": "secret",
	})

	require.NoError(t, err)
	assert.True(t, cfg.SignupOpen())
	assert.True(t, cfg.SignupTurnstileEnabled())
	assert.Equal(t, "site", cfg.SignupTurnstileSiteKey)
	assert.Equal(t, "secret", cfg.SignupTurnstileSecret)
}

func TestMaskSensitiveValue_HidesTheTurnstileSecret(t *testing.T) {
	assert.Equal(t, "*****", maskSensitiveValue("SIGNUP_TURNSTILE_SECRET", "secret"))
}
