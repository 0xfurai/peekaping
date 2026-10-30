package config

import (
	"fmt"
	"strconv"
)

// ValidateMetricsPort checks METRICS_PORT, the port that serves /metrics instead of the API port.
// Unset is fine. A set port must be a TCP port that differs from SERVER_PORT, and needs
// METRICS_ENABLED=true: without it nothing would listen there, which hides a setup that doesn't
// do what its operator thinks it does. It stops startup, not the first scrape.
func ValidateMetricsPort(metricsEnabled bool, metricsPort, serverPort string) error {
	if metricsPort == "" {
		return nil
	}
	port, err := strconv.Atoi(metricsPort)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("METRICS_PORT must be a port from 1 to 65535, got %q", metricsPort)
	}
	if serverPort, err := strconv.Atoi(serverPort); err == nil && serverPort == port {
		return fmt.Errorf("METRICS_PORT must differ from SERVER_PORT, both are %d", port)
	}
	if !metricsEnabled {
		return fmt.Errorf("METRICS_PORT needs METRICS_ENABLED=true: /metrics is off, so nothing would listen on port %d", port)
	}
	return nil
}
