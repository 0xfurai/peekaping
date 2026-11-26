package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validatedConfig loads a config that passes Validate, for a test to break one setting of
func validatedConfig(t *testing.T) *Config {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	return cfg
}

func TestValidate_NamesTheSettingThatIsWrong(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(*Config)
		want   string
	}{
		"postgres without a host": {func(c *Config) { c.DBType = "postgres" }, "database validation failed: DB_HOST is required for postgres database"},
		"postgres without a port": {func(c *Config) { c.DBType, c.DBHost = "postgres", "db" }, "database validation failed: DB_PORT is required for postgres database"},
		"max attempts":            {func(c *Config) { c.BruteforceMaxAttempts = 0 }, "BRUTEFORCE_MAX_ATTEMPTS must be greater than 0"},
		"window":                  {func(c *Config) { c.BruteforceWindow = 0 }, "BRUTEFORCE_WINDOW must be a positive duration"},
		"lockout":                 {func(c *Config) { c.BruteforceLockout = -time.Second }, "BRUTEFORCE_LOCKOUT must be a positive duration"},
		"account max attempts":    {func(c *Config) { c.BruteforceAccountMaxAttempts = -1 }, "BRUTEFORCE_ACCOUNT_MAX_ATTEMPTS must be greater than 0"},
		"account window":          {func(c *Config) { c.BruteforceAccountWindow = -1 }, "BRUTEFORCE_ACCOUNT_WINDOW must be a positive duration"},
		"account lockout":         {func(c *Config) { c.BruteforceAccountLockout = -1 }, "BRUTEFORCE_ACCOUNT_LOCKOUT must be a positive duration"},
		"metrics port":            {func(c *Config) { c.MetricsEnabled, c.MetricsPort = true, c.Port }, "METRICS_PORT"},
		"trusted proxies":         {func(c *Config) { c.TrustedProxies = "not-an-ip" }, "not-an-ip"},
		"cors origins":            {func(c *Config) { c.CORSOrigins = "not an origin" }, "not an origin"},
		"short bootstrap token":   {func(c *Config) { c.ProbeBootstrapToken = "short" }, "PROBE_BOOTSTRAP_TOKEN must be at least 32 characters"},
		"probe offline too soon":  {func(c *Config) { c.ProbeOfflineAfter = time.Minute }, "PROBE_OFFLINE_AFTER must be at least 2m0s"},
		"probe offline too late":  {func(c *Config) { c.ProbeOfflineAfter = 25 * time.Hour }, "PROBE_OFFLINE_AFTER must be at most 24h0m0s"},
		"sender without from":     {func(c *Config) { c.AccountSMTPHost = "smtp.example.com" }, "ACCOUNT_SMTP_FROM"},
		"open signup, no sender":  {func(c *Config) { c.SignupMode = "open" }, "SIGNUP_MODE"},
		"unknown monitor type":    {func(c *Config) { c.DisabledMonitorTypes = "nope" }, "nope"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := validatedConfig(t)
			tc.change(cfg)

			err := Validate(cfg)

			require.Error(t, err)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// Validate stops at the first setting that is wrong, in the order the settings are checked
func TestValidate_ReportsTheFirstSettingThatIsWrong(t *testing.T) {
	cfg := validatedConfig(t)
	cfg.DBType, cfg.DBHost = "postgres", ""
	cfg.BruteforceAccountLockout = 0
	cfg.ProbeOfflineAfter = time.Second
	cfg.DisabledMonitorTypes = "nope"
	assert.EqualError(t, Validate(cfg), "database validation failed: DB_HOST is required for postgres database")

	cfg.DBType = "sqlite"
	cfg.BruteforceMaxAttempts = 0
	assert.EqualError(t, Validate(cfg), "BRUTEFORCE_MAX_ATTEMPTS must be greater than 0")

	cfg.BruteforceMaxAttempts = 20
	cfg.MetricsEnabled, cfg.MetricsPort = true, cfg.Port
	cfg.TrustedProxies = "not-an-ip"
	assert.ErrorContains(t, Validate(cfg), "BRUTEFORCE_ACCOUNT_LOCKOUT must be a positive duration")

	cfg.BruteforceAccountLockout = time.Minute
	assert.ErrorContains(t, Validate(cfg), "METRICS_PORT")

	cfg.MetricsPort = ""
	assert.ErrorContains(t, Validate(cfg), "not-an-ip")

	cfg.TrustedProxies = ""
	assert.EqualError(t, Validate(cfg), "PROBE_OFFLINE_AFTER must be at least 2m0s")

	cfg.ProbeOfflineAfter = 3 * time.Minute
	assert.ErrorContains(t, Validate(cfg), "nope")
}
