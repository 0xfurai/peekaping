package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateMetricsPort(t *testing.T) {
	tests := []struct {
		name                string
		enabled             bool
		port, serverPort    string
		wantErrorContaining string
	}{
		{name: "unset", serverPort: "8034"},
		{name: "unset with metrics on", enabled: true, serverPort: "8034"},
		{name: "a port", enabled: true, port: "9464", serverPort: "8034"},
		{name: "the lowest port", enabled: true, port: "1", serverPort: "8034"},
		{name: "the highest port", enabled: true, port: "65535", serverPort: "8034"},
		{name: "not a number", enabled: true, port: "prometheus", wantErrorContaining: `METRICS_PORT must be a port from 1 to 65535, got "prometheus"`},
		{name: "with a colon", enabled: true, port: ":9464", wantErrorContaining: "METRICS_PORT must be a port from 1 to 65535"},
		{name: "with a plus sign", enabled: true, port: "+9100", wantErrorContaining: `METRICS_PORT must be a port from 1 to 65535, got "+9100"`},
		{name: "with a space", enabled: true, port: " 9100", wantErrorContaining: "METRICS_PORT must be a port from 1 to 65535"},
		{name: "zero", enabled: true, port: "0", wantErrorContaining: "METRICS_PORT must be a port from 1 to 65535"},
		{name: "negative", enabled: true, port: "-1", wantErrorContaining: "METRICS_PORT must be a port from 1 to 65535"},
		{name: "above 65535", enabled: true, port: "65536", wantErrorContaining: "METRICS_PORT must be a port from 1 to 65535"},
		{name: "the server port", enabled: true, port: "8034", serverPort: "8034", wantErrorContaining: "METRICS_PORT must differ from SERVER_PORT"},
		{name: "the server port written with a zero", enabled: true, port: "08034", serverPort: "8034", wantErrorContaining: "METRICS_PORT must differ from SERVER_PORT"},
		{name: "metrics off", port: "9464", serverPort: "8034", wantErrorContaining: "METRICS_PORT needs METRICS_ENABLED=true"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMetricsPort(tc.enabled, tc.port, tc.serverPort)
			if tc.wantErrorContaining == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tc.wantErrorContaining)
		})
	}
}
