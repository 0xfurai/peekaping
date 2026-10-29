package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAccountSMTP(t *testing.T) {
	tests := []struct {
		name                string
		host, from, tls     string
		wantErrorContaining string
	}{
		{name: "unset: account emails are off", host: "", from: "", tls: "starttls"},
		{name: "a host with a sender", host: "smtp.example.com", from: "Peekaping <no-reply@example.com>", tls: "starttls"},
		{name: "a host without a sender", host: "smtp.example.com", from: "", tls: "starttls", wantErrorContaining: "ACCOUNT_SMTP_FROM is required when ACCOUNT_SMTP_HOST is set"},
		{name: "a sender that is not an address", host: "smtp.example.com", from: "not an address", tls: "starttls", wantErrorContaining: "ACCOUNT_SMTP_FROM"},
		{name: "implicit TLS", host: "smtp.example.com", from: "no-reply@example.com", tls: "tls"},
		{name: "no TLS", host: "localhost", from: "no-reply@example.com", tls: "none"},
		{name: "an unknown TLS mode", host: "smtp.example.com", from: "no-reply@example.com", tls: "ssl", wantErrorContaining: "ACCOUNT_SMTP_TLS must be one of: starttls, tls, none"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAccountSMTP(tc.host, tc.from, tc.tls)
			if tc.wantErrorContaining == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tc.wantErrorContaining)
		})
	}
}

func TestLoadConfig_AccountSMTPDefaultsToOff(t *testing.T) {
	cfg, err := loadTestConfig(t, nil)

	require.NoError(t, err)
	assert.False(t, cfg.AccountEmailsEnabled())
	assert.Equal(t, 587, cfg.AccountSMTPPort)
	assert.Equal(t, "starttls", cfg.AccountSMTPTLS)
}

func TestLoadConfig_AccountSMTPIsSetByTheEnvironment(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{
		"ACCOUNT_SMTP_HOST": "smtp.example.com", "ACCOUNT_SMTP_PORT": "465", "ACCOUNT_SMTP_USER": "mailer",
		"ACCOUNT_SMTP_PASSWORD": "secret", "ACCOUNT_SMTP_FROM": "no-reply@example.com", "ACCOUNT_SMTP_TLS": "tls",
	})

	require.NoError(t, err)
	assert.True(t, cfg.AccountEmailsEnabled())
	assert.Equal(t, "smtp.example.com", cfg.AccountSMTPHost)
	assert.Equal(t, 465, cfg.AccountSMTPPort)
	assert.Equal(t, "mailer", cfg.AccountSMTPUser)
	assert.Equal(t, "secret", cfg.AccountSMTPPassword)
	assert.Equal(t, "no-reply@example.com", cfg.AccountSMTPFrom)
	assert.Equal(t, "tls", cfg.AccountSMTPTLS)
}

func TestLoadConfig_AccountSMTPRefusesAnUnknownTLSModeOrPort(t *testing.T) {
	_, err := loadTestConfig(t, map[string]string{"ACCOUNT_SMTP_TLS": "ssl"})
	assert.ErrorContains(t, err, "AccountSMTPTLS")

	_, err = loadTestConfig(t, map[string]string{"ACCOUNT_SMTP_PORT": "70000"})
	assert.ErrorContains(t, err, "AccountSMTPPort")
}

func TestMaskSensitiveValue_HidesTheAccountSMTPPassword(t *testing.T) {
	assert.Equal(t, "*****", maskSensitiveValue("ACCOUNT_SMTP_PASSWORD", "secret"))
}
