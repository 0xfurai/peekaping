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

func TestLoadConfig_TelemetryEndpointDefaultsToPeekapingsWorker(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"TELEMETRY_URL": ""})

	require.NoError(t, err)
	assert.Equal(t, "https://telemetry.peekaping.com/v1/ping", cfg.TelemetryURL)
}

func TestLoadConfig_TelemetryEndpointIsSetByTheEnvironment(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"TELEMETRY_URL": "http://localhost:8787/v1/ping"})

	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8787/v1/ping", cfg.TelemetryURL)
}

func TestLoadConfig_TelemetryKillSwitchesComeFromTheEnvironment(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"TELEMETRY": "off", "DO_NOT_TRACK": "1"})

	require.NoError(t, err)
	assert.Equal(t, "off", cfg.Telemetry)
	assert.Equal(t, "1", cfg.DoNotTrack)
}

func TestConfig_TelemetryForcedOff(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		telemetry, doNotTrack string
		want                  bool
	}{
		{"nothing set", "", "", false},
		{"TELEMETRY=on", "on", "", false},
		{"TELEMETRY=true", "true", "", false},
		{"TELEMETRY=off", "off", "", true},
		{"TELEMETRY=OFF", "OFF", "", true},
		{"TELEMETRY=false", "false", "", true},
		{"TELEMETRY=False", "False", "", true},
		{"TELEMETRY=0", "0", "", true},
		{"TELEMETRY=no", "no", "", true},
		{"TELEMETRY=No", "No", "", true},
		{"TELEMETRY=off with spaces", " off ", "", true},
		{"TELEMETRY=1 is not an off value", "1", "", false},
		{"TELEMETRY=anything else", "maybe", "", false},
		{"DO_NOT_TRACK=1", "", "1", true},
		{"DO_NOT_TRACK=true", "", "true", true},
		{"DO_NOT_TRACK=TRUE", "", "TRUE", true},
		{"DO_NOT_TRACK=0", "", "0", false},
		{"DO_NOT_TRACK=false", "", "false", false},
		{"DO_NOT_TRACK=yes is not a kill switch value", "", "yes", false},
		{"TELEMETRY=on cannot undo DO_NOT_TRACK=1", "on", "1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Telemetry: tc.telemetry, DoNotTrack: tc.doNotTrack}

			assert.Equal(t, tc.want, cfg.TelemetryForcedOff())
		})
	}
}
