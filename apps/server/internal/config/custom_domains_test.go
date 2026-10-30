package config_test

import (
	"testing"

	"peekaping/internal/config"

	"github.com/stretchr/testify/assert"
)

func TestValidateCustomDomainCNAMETarget(t *testing.T) {
	for _, ok := range []string{"", "  ", "status.example.com", "Status.Example.COM.", "a-b.example.com"} {
		assert.NoError(t, config.ValidateCustomDomainCNAMETarget(ok), ok)
	}
	for _, bad := range []string{"https://status.example.com", "status.example.com:443", "10.0.0.1", "::1", "-a.example.com", "a..example.com", "a b.example.com", "status.example.com/path"} {
		assert.Error(t, config.ValidateCustomDomainCNAMETarget(bad), bad)
	}
}

func TestValidateVerifyCustomDomains(t *testing.T) {
	for _, ok := range []string{"", "true", "1", "false", "0"} {
		assert.NoError(t, config.ValidateVerifyCustomDomains(ok), ok)
	}
	// The shared parser reads each of these as false, which would turn verification off unasked
	for _, bad := range []string{"TRUE", "True", "yes", "on", "enabled", " true", "true ", "2", "FALSE"} {
		err := config.ValidateVerifyCustomDomains(bad)
		assert.ErrorContains(t, err, "VERIFY_CUSTOM_DOMAINS must be true, 1, false or 0", bad)
	}
}

func TestCustomDomainCNAMETargetFor(t *testing.T) {
	for name, tc := range map[string]struct{ configured, publicURL, want string }{
		"the configured target wins, normalized": {"Status.Example.COM.", "https://app.example.com", "status.example.com"},
		"the host of the public URL":             {"", "https://Peek.Example.com/status", "peek.example.com"},
		"the host without its port":              {"", "https://peek.example.com:8443", "peek.example.com"},
		"localhost has none":                     {"", "http://localhost:8383", ""},
		"an IPv4 address has none":               {"", "http://192.168.1.5:8383", ""},
		"an IPv6 address has none":               {"", "http://[::1]:8383", ""},
		"no public URL has none":                 {"", "", ""},
		"the configured target beats localhost":  {"status.example.com", "http://localhost", "status.example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, config.CustomDomainCNAMETargetFor(tc.configured, tc.publicURL))
		})
	}
}
