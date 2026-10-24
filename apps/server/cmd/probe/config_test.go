package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadConfig loads the probe's configuration from env alone: the variables the probe reads that
// env leaves out are cleared, so that the test doesn't depend on the machine's
func loadConfig(t *testing.T, env map[string]string) (*Config, error) {
	t.Helper()
	for _, name := range []string{
		"PEEKAPING_URL", "PROBE_TOKEN", "PROBE_ALLOW_INSECURE", "PROBE_CONCURRENCY", "PROBE_READY_ADDR", "MODE", "LOG_LEVEL",
	} {
		t.Setenv(name, "")
	}
	for name, value := range env {
		t.Setenv(name, value)
	}
	return LoadAndValidate(t.TempDir())
}

// minimal is the least a probe needs to start
func minimal() map[string]string {
	return map[string]string{"PEEKAPING_URL": "https://peekaping.example.com", "PROBE_TOKEN": "secret-token"}
}

func with(env map[string]string, name, value string) map[string]string {
	env[name] = value
	return env
}

func TestConfig_StartsWithTheURLAndTheToken(t *testing.T) {
	cfg, err := loadConfig(t, minimal())

	require.NoError(t, err)
	assert.Equal(t, "https://peekaping.example.com", cfg.URL)
	assert.Equal(t, "secret-token", cfg.Token)
}

func TestConfig_DefaultsConcurrencyReadyAddrAndLogLevel(t *testing.T) {
	cfg, err := loadConfig(t, minimal())

	require.NoError(t, err)
	assert.Equal(t, 128, cfg.Concurrency)
	assert.Equal(t, "127.0.0.1:8081", cfg.ReadyAddr)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "prod", cfg.Mode, "a probe started with docker run logs as in production")
	assert.False(t, cfg.AllowInsecure)
}

func TestConfig_ReadsConcurrencyReadyAddrAndLogLevel(t *testing.T) {
	env := minimal()
	env["PROBE_CONCURRENCY"] = "16"
	env["PROBE_READY_ADDR"] = "0.0.0.0:9000"
	env["LOG_LEVEL"] = "debug"

	cfg, err := loadConfig(t, env)

	require.NoError(t, err)
	assert.Equal(t, 16, cfg.Concurrency)
	assert.Equal(t, "0.0.0.0:9000", cfg.ReadyAddr)
	assert.Equal(t, "debug", cfg.LogLevel)
}

func TestConfig_MissingURLIsReportedByItsName(t *testing.T) {
	_, err := loadConfig(t, map[string]string{"PROBE_TOKEN": "secret-token"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PEEKAPING_URL is required")
	assert.NotContains(t, err.Error(), "PROBE_TOKEN")
}

func TestConfig_MissingTokenIsReportedByItsName(t *testing.T) {
	_, err := loadConfig(t, map[string]string{"PEEKAPING_URL": "https://peekaping.example.com"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PROBE_TOKEN is required")
	assert.NotContains(t, err.Error(), "PEEKAPING_URL")
}

func TestConfig_MissingBothReportsBoth(t *testing.T) {
	_, err := loadConfig(t, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PEEKAPING_URL is required")
	assert.Contains(t, err.Error(), "PROBE_TOKEN is required")
}

// A token read from a file or a secret often ends in a newline, and validation passes it: the
// probe must run with the value it validated, not send the newline to the server
func TestConfig_TheURLAndTheTokenAreTrimmedAsValidated(t *testing.T) {
	env := with(minimal(), "PEEKAPING_URL", "  https://peekaping.example.com/base\n")
	env["PROBE_TOKEN"] = "secret-token\n"

	cfg, err := loadConfig(t, env)

	require.NoError(t, err)
	assert.Equal(t, "https://peekaping.example.com/base", cfg.URL)
	assert.Equal(t, "secret-token", cfg.Token)
}

func TestConfig_AWhitespaceTokenIsMissing(t *testing.T) {
	_, err := loadConfig(t, with(minimal(), "PROBE_TOKEN", "  "))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PROBE_TOKEN is required")
}

func TestConfig_APlainHTTPURLIsRefusedUnlessTheHostIsThisMachine(t *testing.T) {
	refused := []string{
		"http://peekaping.example.com",
		"http://10.0.0.5:8034",
		"http://192.168.1.10",
		"http://localhost.example.com",
		"http://127.0.0.1.example.com",
		"http://127.0.0.2:8034",
	}
	for _, url := range refused {
		t.Run(url, func(t *testing.T) {
			_, err := loadConfig(t, with(minimal(), "PEEKAPING_URL", url))

			require.Error(t, err)
			assert.Contains(t, err.Error(), "PROBE_ALLOW_INSECURE=true")
		})
	}
}

func TestConfig_APlainHTTPURLToThisMachineIsAllowed(t *testing.T) {
	allowed := []string{
		"http://localhost",
		"http://localhost:8034",
		"http://LOCALHOST:8034",
		"http://127.0.0.1:8034",
		"http://[::1]:8034",
	}
	for _, url := range allowed {
		t.Run(url, func(t *testing.T) {
			cfg, err := loadConfig(t, with(minimal(), "PEEKAPING_URL", url))

			require.NoError(t, err)
			assert.Equal(t, url, cfg.URL)
		})
	}
}

func TestConfig_AllowInsecureAllowsAPlainHTTPURLToAnyHost(t *testing.T) {
	env := with(minimal(), "PEEKAPING_URL", "http://peekaping.example.com:8034")
	env["PROBE_ALLOW_INSECURE"] = "true"

	cfg, err := loadConfig(t, env)

	require.NoError(t, err)
	assert.True(t, cfg.AllowInsecure)
	assert.Equal(t, "http://peekaping.example.com:8034", cfg.URL)
}

func TestConfig_APlainHTTPErrorDoesNotRepeatTheURL(t *testing.T) {
	_, err := loadConfig(t, with(minimal(), "PEEKAPING_URL", "http://user:hunter2@peekaping.example.com"))

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "hunter2")
	assert.Contains(t, err.Error(), "peekaping.example.com")
}

func TestConfig_AnHTTPSURLIsAllowed(t *testing.T) {
	cfg, err := loadConfig(t, with(minimal(), "PEEKAPING_URL", "https://peekaping.example.com:8443/base"))

	require.NoError(t, err)
	assert.Equal(t, "https://peekaping.example.com:8443/base", cfg.URL)
}

func TestConfig_AURLThatIsNotAnHTTPAddressIsRefused(t *testing.T) {
	for _, url := range []string{"peekaping.example.com", "ftp://peekaping.example.com", "https://", "http://:8034", "://x"} {
		t.Run(url, func(t *testing.T) {
			_, err := loadConfig(t, with(minimal(), "PEEKAPING_URL", url))

			require.Error(t, err)
			assert.Contains(t, err.Error(), "PEEKAPING_URL must be an address starting with https://")
		})
	}
}

func TestConfig_ABadConcurrencyReadyAddrOrLogLevelIsReportedByItsName(t *testing.T) {
	cases := map[string]string{
		"PROBE_CONCURRENCY": "-4",
		"PROBE_READY_ADDR":  "8081",
		"LOG_LEVEL":         "loud",
		"MODE":              "staging",
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadConfig(t, with(minimal(), name, value))

			require.Error(t, err)
			assert.Contains(t, err.Error(), name+" must")
		})
	}
}

func TestConfig_ReadyAddrNeedsAPortInRange(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:http"} {
		t.Run(addr, func(t *testing.T) {
			_, err := loadConfig(t, with(minimal(), "PROBE_READY_ADDR", addr))

			require.Error(t, err)
			assert.Contains(t, err.Error(), "PROBE_READY_ADDR must be")
		})
	}
}

func TestConfig_LogLevelTakesWhatTheOtherBinariesTake(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "warning", "error", "dpanic", "panic", "fatal", "INFO", "Warning"} {
		t.Run(level, func(t *testing.T) {
			cfg, err := loadConfig(t, with(minimal(), "LOG_LEVEL", level))

			require.NoError(t, err)
			assert.Equal(t, level, cfg.LogLevel)
		})
	}
}
