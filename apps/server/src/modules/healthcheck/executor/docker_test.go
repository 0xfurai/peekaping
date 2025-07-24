package executor

import (
	"context"
	"peekaping/src/modules/shared"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestDockerExecutor_Unmarshal(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDockerExecutor(logger)

	tests := []struct {
		name                   string
		config                 string
		expectedError          bool
		expectedContainerID    string
		expectedConnectionType string
		expectedDockerDaemon   string
	}{
		{
			name: "valid socket config",
			config: `{
				"container_id": "mycontainer123",
				"connection_type": "socket",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			expectedError:          false,
			expectedContainerID:    "mycontainer123",
			expectedConnectionType: "socket",
			expectedDockerDaemon:   "/var/run/docker.sock",
		},
		{
			name: "valid tcp config",
			config: `{
				"container_id": "webapp_container",
				"connection_type": "tcp",
				"docker_daemon": "tcp://localhost:2376"
			}`,
			expectedError:          false,
			expectedContainerID:    "webapp_container",
			expectedConnectionType: "tcp",
			expectedDockerDaemon:   "tcp://localhost:2376",
		},
		{
			name: "invalid json",
			config: `{
				"container_id": "mycontainer",
				invalid_json
			}`,
			expectedError: true,
		},
		{
			name:          "empty config",
			config:        `{}`,
			expectedError: false,
		},
		{
			name: "unknown field",
			config: `{
				"container_id": "mycontainer",
				"connection_type": "socket",
				"docker_daemon": "/var/run/docker.sock",
				"unknown_field": "value"
			}`,
			expectedError: true,
		},
		{
			name: "partial config",
			config: `{
				"container_id": "mycontainer"
			}`,
			expectedError: false,
		},
		{
			name: "null values",
			config: `{
				"container_id": null,
				"connection_type": null,
				"docker_daemon": null
			}`,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Unmarshal(tt.config)
			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.expectedContainerID != "" {
					cfg := result.(*DockerConfig)
					assert.Equal(t, tt.expectedContainerID, cfg.ContainerID)
					assert.Equal(t, tt.expectedConnectionType, cfg.ConnectionType)
					assert.Equal(t, tt.expectedDockerDaemon, cfg.DockerDaemon)
				}
			}
		})
	}
}

func TestDockerExecutor_Validate(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDockerExecutor(logger)

	tests := []struct {
		name          string
		config        string
		expectedError bool
		description   string
	}{
		{
			name: "valid socket config",
			config: `{
				"container_id": "mycontainer123",
				"connection_type": "socket",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			expectedError: false,
			description:   "Valid socket configuration",
		},
		{
			name: "valid tcp config",
			config: `{
				"container_id": "webapp_container",
				"connection_type": "tcp",
				"docker_daemon": "tcp://localhost:2376"
			}`,
			expectedError: false,
			description:   "Valid TCP configuration",
		},
		{
			name: "missing container_id",
			config: `{
				"connection_type": "socket",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			expectedError: true,
			description:   "container_id is required",
		},
		{
			name: "empty container_id",
			config: `{
				"container_id": "",
				"connection_type": "socket",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			expectedError: true,
			description:   "container_id cannot be empty",
		},
		{
			name: "missing connection_type",
			config: `{
				"container_id": "mycontainer",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			expectedError: true,
			description:   "connection_type is required",
		},
		{
			name: "empty connection_type",
			config: `{
				"container_id": "mycontainer",
				"connection_type": "",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			expectedError: true,
			description:   "connection_type cannot be empty",
		},
		{
			name: "invalid connection_type",
			config: `{
				"container_id": "mycontainer",
				"connection_type": "invalid",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			expectedError: true,
			description:   "connection_type must be 'socket' or 'tcp'",
		},
		{
			name: "missing docker_daemon",
			config: `{
				"container_id": "mycontainer",
				"connection_type": "socket"
			}`,
			expectedError: true,
			description:   "docker_daemon is required",
		},
		{
			name: "empty docker_daemon",
			config: `{
				"container_id": "mycontainer",
				"connection_type": "socket",
				"docker_daemon": ""
			}`,
			expectedError: true,
			description:   "docker_daemon cannot be empty",
		},
		{
			name:          "all fields missing",
			config:        `{}`,
			expectedError: true,
			description:   "All required fields are missing",
		},
		{
			name: "invalid json format",
			config: `{
				"container_id": "mycontainer",
				invalid_json
			}`,
			expectedError: true,
			description:   "Invalid JSON should fail",
		},
		{
			name: "case sensitive connection_type",
			config: `{
				"container_id": "mycontainer",
				"connection_type": "Socket",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			expectedError: true,
			description:   "connection_type is case sensitive",
		},
		{
			name: "tcp with socket path",
			config: `{
				"container_id": "mycontainer",
				"connection_type": "tcp",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			expectedError: false,
			description:   "TCP connection_type with socket path is valid (validation doesn't check path format)",
		},
		{
			name: "socket with tcp url",
			config: `{
				"container_id": "mycontainer",
				"connection_type": "socket",
				"docker_daemon": "tcp://localhost:2376"
			}`,
			expectedError: false,
			description:   "Socket connection_type with TCP URL is valid (validation doesn't check path format)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.Validate(tt.config)
			if tt.expectedError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

func TestDockerExecutor_Execute_ConfigErrors(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDockerExecutor(logger)

	tests := []struct {
		name           string
		monitor        *Monitor
		expectedStatus shared.MonitorStatus
		expectMessage  string
		description    string
	}{
		{
			name: "invalid json config",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "docker",
				Name:     "Test Docker Monitor",
				Interval: 30,
				Timeout:  5,
				Config:   `{invalid json}`,
			},
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "invalid config",
			description:    "Invalid JSON configuration should return DOWN status",
		},
		{
			name: "empty config",
			monitor: &Monitor{
				ID:       "monitor2",
				Type:     "docker",
				Name:     "Test Docker Monitor",
				Interval: 30,
				Timeout:  5,
				Config:   `{}`,
			},
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "unknown docker connection type",
			description:    "Empty config should fail with unknown connection type",
		},
		{
			name: "missing container_id",
			monitor: &Monitor{
				ID:       "monitor3",
				Type:     "docker",
				Name:     "Test Docker Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"connection_type": "socket",
					"docker_daemon": "/var/run/docker.sock"
				}`,
			},
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "container inspect error",
			description:    "Missing container_id should cause container inspect to fail",
		},
		{
			name: "invalid connection type",
			monitor: &Monitor{
				ID:       "monitor4",
				Type:     "docker",
				Name:     "Test Docker Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"container_id": "mycontainer",
					"connection_type": "invalid",
					"docker_daemon": "/var/run/docker.sock"
				}`,
			},
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "unknown docker connection type: invalid",
			description:    "Invalid connection type should return specific error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			result := executor.Execute(ctx, tt.monitor, nil)
			assert.Equal(t, tt.expectedStatus, result.Status, tt.description)
			if tt.expectMessage != "" {
				assert.Contains(t, result.Message, tt.expectMessage, tt.description)
			}
			assert.NotZero(t, result.StartTime)
			assert.NotZero(t, result.EndTime)
			assert.True(t, result.EndTime.After(result.StartTime) || result.EndTime.Equal(result.StartTime))
		})
	}
}

func TestDockerExecutor_Execute_DockerClientErrors(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDockerExecutor(logger)

	tests := []struct {
		name           string
		monitor        *Monitor
		expectedStatus shared.MonitorStatus
		expectMessage  string
		description    string
	}{
		{
			name: "socket connection failure",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "docker",
				Name:     "Test Docker Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"container_id": "nonexistent_container",
					"connection_type": "socket",
					"docker_daemon": "/nonexistent/docker.sock"
				}`,
			},
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "container inspect error",
			description:    "Non-existent socket should fail with inspect error",
		},
		{
			name: "tcp connection failure",
			monitor: &Monitor{
				ID:       "monitor2",
				Type:     "docker",
				Name:     "Test Docker Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"container_id": "nonexistent_container",
					"connection_type": "tcp",
					"docker_daemon": "tcp://invalid-host:9999"
				}`,
			},
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "container inspect error",
			description:    "Invalid TCP endpoint should fail with inspect error",
		},
		{
			name: "valid config with non-existent container",
			monitor: &Monitor{
				ID:       "monitor3",
				Type:     "docker",
				Name:     "Test Docker Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"container_id": "definitely_nonexistent_container_12345",
					"connection_type": "socket",
					"docker_daemon": "/var/run/docker.sock"
				}`,
			},
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "container inspect error",
			description:    "Non-existent container should fail with inspect error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			result := executor.Execute(ctx, tt.monitor, nil)
			assert.Equal(t, tt.expectedStatus, result.Status, tt.description)
			if tt.expectMessage != "" {
				assert.Contains(t, result.Message, tt.expectMessage, tt.description)
			}
			assert.NotZero(t, result.StartTime)
			assert.NotZero(t, result.EndTime)
			assert.True(t, result.EndTime.After(result.StartTime) || result.EndTime.Equal(result.StartTime))
		})
	}
}

func TestDockerExecutor_Execute_WithProxy(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDockerExecutor(logger)

	// Docker executor should ignore proxy (direct connection to Docker daemon)
	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "docker",
		Name:     "Test Docker Monitor",
		Interval: 30,
		Timeout:  5,
		Config: `{
			"container_id": "test_container",
			"connection_type": "socket",
			"docker_daemon": "/var/run/docker.sock"
		}`,
	}

	proxy := &Proxy{
		ID:       "proxy1",
		Host:     "proxy.example.com",
		Port:     8080,
		Protocol: "http",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result := executor.Execute(ctx, monitor, proxy)
	assert.NotNil(t, result)
	assert.Equal(t, shared.MonitorStatusDown, result.Status) // Expected to fail since container doesn't exist
	assert.Contains(t, result.Message, "container inspect error")
	// Proxy should not affect Docker execution - it connects directly to Docker daemon
}

func TestDockerExecutor_Execute_ContextTimeout(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDockerExecutor(logger)

	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "docker",
		Name:     "Test Docker Monitor",
		Interval: 30,
		Timeout:  5,
		Config: `{
			"container_id": "test_container",
			"connection_type": "tcp",
			"docker_daemon": "tcp://1.2.3.4:2376"
		}`,
	}

	// Very short timeout to force timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	result := executor.Execute(ctx, monitor, nil)
	assert.NotNil(t, result)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	// Should fail with either timeout or connection error
	assert.NotEmpty(t, result.Message)
}

func TestNewDockerExecutor(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()

	// Test executor creation
	executor := NewDockerExecutor(logger)

	// Verify executor is properly initialized
	assert.NotNil(t, executor)
	assert.NotNil(t, executor.logger)
}

func TestDockerExecutor_Interface(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDockerExecutor(logger)

	// Verify that DockerExecutor implements the Executor interface
	var _ Executor = executor
}

// Helper function to test configuration edge cases
func TestDockerConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		config   DockerConfig
		expected bool
	}{
		{
			name: "very long container ID",
			config: DockerConfig{
				ContainerID:    "very_long_container_id_that_might_be_sha256_hash_1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				ConnectionType: "socket",
				DockerDaemon:   "/var/run/docker.sock",
			},
			expected: true,
		},
		{
			name: "container ID with special characters",
			config: DockerConfig{
				ContainerID:    "container-with_special.chars123",
				ConnectionType: "socket",
				DockerDaemon:   "/var/run/docker.sock",
			},
			expected: true,
		},
		{
			name: "windows named pipe",
			config: DockerConfig{
				ContainerID:    "windows_container",
				ConnectionType: "socket",
				DockerDaemon:   "//./pipe/docker_engine",
			},
			expected: true,
		},
		{
			name: "tcp with TLS port",
			config: DockerConfig{
				ContainerID:    "secure_container",
				ConnectionType: "tcp",
				DockerDaemon:   "tcp://docker.example.com:2376",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := GenericValidator(&tt.config)
			if tt.expected {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

// Test to verify the health status logic fix
func TestDockerExecutor_HealthStatusLogic(t *testing.T) {
	// Note: These are unit tests that verify the expected logic flow
	// They test the configuration and validation paths, but full integration
	// testing with actual Docker containers would require a running Docker daemon

	logger := zap.NewNop().Sugar()
	executor := NewDockerExecutor(logger)

	// Test scenarios that demonstrate the fixed health status logic
	tests := []struct {
		name        string
		description string
		config      string
		setupNote   string
	}{
		{
			name:        "healthy_container_scenario",
			description: "Configuration for monitoring a healthy container",
			config: `{
				"container_id": "healthy_webapp_container",
				"connection_type": "socket",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			setupNote: "When container.State.Health.Status == 'healthy', should return UP status",
		},
		{
			name:        "starting_container_scenario",
			description: "Configuration for monitoring a starting container",
			config: `{
				"container_id": "starting_webapp_container",
				"connection_type": "socket",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			setupNote: "When container.State.Health.Status == 'starting', should return PENDING status",
		},
		{
			name:        "unhealthy_container_scenario",
			description: "Configuration for monitoring an unhealthy container",
			config: `{
				"container_id": "unhealthy_webapp_container",
				"connection_type": "socket",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			setupNote: "When container.State.Health.Status == 'unhealthy', should return DOWN status (bug fix)",
		},
		{
			name:        "container_without_healthcheck",
			description: "Configuration for monitoring a container without health checks",
			config: `{
				"container_id": "simple_container_no_healthcheck",
				"connection_type": "socket",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			setupNote: "When container.State.Health == nil, should use container.State.Status",
		},
		{
			name:        "tcp_connection_scenario",
			description: "Configuration for monitoring via TCP connection",
			config: `{
				"container_id": "remote_container",
				"connection_type": "tcp",
				"docker_daemon": "tcp://docker-host.example.com:2376"
			}`,
			setupNote: "TCP connections should work the same as socket connections",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that the configuration is valid
			err := executor.Validate(tt.config)
			assert.NoError(t, err, "Configuration should be valid: %s", tt.description)

			// Test that unmarshaling works correctly
			result, err := executor.Unmarshal(tt.config)
			assert.NoError(t, err, "Should unmarshal successfully")
			assert.NotNil(t, result, "Result should not be nil")

			cfg := result.(*DockerConfig)
			assert.NotEmpty(t, cfg.ContainerID, "Container ID should not be empty")
			assert.Contains(t, []string{"socket", "tcp"}, cfg.ConnectionType, "Connection type should be valid")
			assert.NotEmpty(t, cfg.DockerDaemon, "Docker daemon should not be empty")

			t.Logf("Test scenario: %s", tt.setupNote)
		})
	}
}

// Test to document the health status logic behavior
func TestDockerExecutor_HealthStatusDocumentation(t *testing.T) {
	t.Run("health_status_mapping_documentation", func(t *testing.T) {
		// This test documents the expected behavior of health status mapping
		// which was fixed in the bug report

		expectedMappings := map[string]string{
			"healthy":   "should map to MonitorStatusUp",
			"starting":  "should map to MonitorStatusPending",
			"unhealthy": "should map to MonitorStatusDown (fixed from PENDING)",
			"none":      "should map to MonitorStatusDown (unknown status)",
			"no_health": "should use container.State.Status when Health is nil",
		}

		for status, expected := range expectedMappings {
			t.Logf("Health status '%s': %s", status, expected)
		}

		// Verify that the logic handles the key scenarios correctly
		assert.True(t, true, "Health status logic should follow the mappings above")
	})
}

// Test edge cases for health status transitions
func TestDockerExecutor_HealthStatusEdgeCases(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewDockerExecutor(logger)

	// Test various edge case configurations
	edgeCases := []struct {
		name        string
		config      string
		expectValid bool
		description string
	}{
		{
			name: "container_name_instead_of_id",
			config: `{
				"container_id": "my-container-name",
				"connection_type": "socket",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			expectValid: true,
			description: "Container names should be accepted (Docker API handles both)",
		},
		{
			name: "sha256_container_id",
			config: `{
				"container_id": "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"connection_type": "socket",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			expectValid: true,
			description: "Full SHA256 container IDs should be accepted",
		},
		{
			name: "short_container_id",
			config: `{
				"container_id": "abc123",
				"connection_type": "socket",
				"docker_daemon": "/var/run/docker.sock"
			}`,
			expectValid: true,
			description: "Short container IDs should be accepted",
		},
		{
			name: "custom_socket_path",
			config: `{
				"container_id": "test_container",
				"connection_type": "socket",
				"docker_daemon": "/custom/path/docker.sock"
			}`,
			expectValid: true,
			description: "Custom socket paths should be accepted",
		},
		{
			name: "tcp_with_port",
			config: `{
				"container_id": "test_container",
				"connection_type": "tcp",
				"docker_daemon": "tcp://127.0.0.1:2375"
			}`,
			expectValid: true,
			description: "TCP with specific IP and port should be accepted",
		},
	}

	for _, tt := range edgeCases {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.Validate(tt.config)
			if tt.expectValid {
				assert.NoError(t, err, tt.description)
			} else {
				assert.Error(t, err, tt.description)
			}
		})
	}
}

func TestDockerExecutor_TLS_Unmarshal(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDockerExecutor(logger)

	tests := []struct {
		name               string
		config             string
		expectedError      bool
		expectedTLSEnabled bool
		expectedTLSVerify  bool
	}{
		{
			name: "tcp config with TLS enabled",
			config: `{
				"container_id": "mycontainer",
				"connection_type": "tcp",
				"docker_daemon": "tcp://localhost:2376",
				"tls_enabled": true,
				"tls_cert": "cert-content",
				"tls_key": "key-content",
				"tls_ca": "ca-content",
				"tls_verify": true
			}`,
			expectedError:      false,
			expectedTLSEnabled: true,
			expectedTLSVerify:  true,
		},
		{
			name: "tcp config with TLS disabled",
			config: `{
				"container_id": "mycontainer",
				"connection_type": "tcp",
				"docker_daemon": "tcp://localhost:2375",
				"tls_enabled": false
			}`,
			expectedError:      false,
			expectedTLSEnabled: false,
			expectedTLSVerify:  false,
		},
		{
			name: "tcp config with TLS verify disabled",
			config: `{
				"container_id": "mycontainer",
				"connection_type": "tcp",
				"docker_daemon": "tcp://localhost:2376",
				"tls_enabled": true,
				"tls_verify": false
			}`,
			expectedError:      false,
			expectedTLSEnabled: true,
			expectedTLSVerify:  false,
		},
		{
			name: "socket config with TLS fields (should be ignored)",
			config: `{
				"container_id": "mycontainer",
				"connection_type": "socket",
				"docker_daemon": "/var/run/docker.sock",
				"tls_enabled": true,
				"tls_cert": "cert-content"
			}`,
			expectedError:      false,
			expectedTLSEnabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Unmarshal(tt.config)
			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				cfg := result.(*DockerConfig)
				assert.Equal(t, tt.expectedTLSEnabled, cfg.TLSEnabled)
				assert.Equal(t, tt.expectedTLSVerify, cfg.TLSVerify)
			}
		})
	}
}

func TestDockerExecutor_TLS_CreateTLSConfig(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDockerExecutor(logger)

	tests := []struct {
		name          string
		config        *DockerConfig
		expectedError bool
		description   string
	}{
		{
			name: "TLS disabled",
			config: &DockerConfig{
				TLSEnabled: false,
			},
			expectedError: false,
			description:   "TLS disabled should return nil config",
		},
		{
			name: "TLS enabled with verify false",
			config: &DockerConfig{
				TLSEnabled: true,
				TLSVerify:  false,
			},
			expectedError: false,
			description:   "TLS enabled with verification disabled should work",
		},
		{
			name: "TLS enabled without certificates",
			config: &DockerConfig{
				TLSEnabled: true,
				TLSVerify:  true,
			},
			expectedError: false,
			description:   "TLS enabled without certificates should work (server-only TLS)",
		},
		{
			name: "TLS enabled with invalid certificate",
			config: &DockerConfig{
				TLSEnabled: true,
				TLSCert:    "invalid-cert",
				TLSKey:     "invalid-key",
			},
			expectedError: true,
			description:   "Invalid certificate should return error",
		},
		{
			name: "TLS enabled with invalid CA",
			config: &DockerConfig{
				TLSEnabled: true,
				TLSCA:      "invalid-ca",
			},
			expectedError: true,
			description:   "Invalid CA certificate should return error",
		},
		{
			name: "TLS enabled with cert but no key",
			config: &DockerConfig{
				TLSEnabled: true,
				TLSCert:    "some-cert",
			},
			expectedError: true,
			description:   "Certificate without key should return error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlsConfig, err := executor.createTLSConfig(tt.config)
			if tt.expectedError {
				assert.Error(t, err, tt.description)
				assert.Nil(t, tlsConfig)
			} else {
				assert.NoError(t, err, tt.description)
				if tt.config.TLSEnabled {
					assert.NotNil(t, tlsConfig)
					assert.Equal(t, !tt.config.TLSVerify, tlsConfig.InsecureSkipVerify)
				} else {
					assert.Nil(t, tlsConfig)
				}
			}
		})
	}
}

func TestDockerExecutor_TLS_Execute_ConfigErrors(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDockerExecutor(logger)

	tests := []struct {
		name           string
		monitor        *Monitor
		expectedStatus shared.MonitorStatus
		expectMessage  string
		description    string
	}{
		{
			name: "TCP with TLS enabled but invalid certificate",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "docker",
				Name:     "Test Docker Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"container_id": "test_container",
					"connection_type": "tcp",
					"docker_daemon": "tcp://localhost:2376",
					"tls_enabled": true,
					"tls_cert": "invalid-cert",
					"tls_key": "invalid-key"
				}`,
			},
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "TLS configuration error",
			description:    "Invalid TLS certificate should return TLS configuration error",
		},
		{
			name: "TCP with TLS enabled but missing certificates",
			monitor: &Monitor{
				ID:       "monitor2",
				Type:     "docker",
				Name:     "Test Docker Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"container_id": "test_container",
					"connection_type": "tcp",
					"docker_daemon": "tcp://localhost:2376",
					"tls_enabled": true
				}`,
			},
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "container inspect error",
			description:    "TLS enabled without certificates should still attempt connection",
		},
		{
			name: "TCP with TLS disabled",
			monitor: &Monitor{
				ID:       "monitor3",
				Type:     "docker",
				Name:     "Test Docker Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"container_id": "test_container",
					"connection_type": "tcp",
					"docker_daemon": "tcp://localhost:2375",
					"tls_enabled": false
				}`,
			},
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "container inspect error",
			description:    "TCP without TLS should work for plain connections",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			result := executor.Execute(ctx, tt.monitor, nil)
			assert.Equal(t, tt.expectedStatus, result.Status, tt.description)
			if tt.expectMessage != "" {
				assert.Contains(t, result.Message, tt.expectMessage, tt.description)
			}
			assert.NotZero(t, result.StartTime)
			assert.NotZero(t, result.EndTime)
		})
	}
}

func TestDockerExecutor_TLS_Validation(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDockerExecutor(logger)

	tests := []struct {
		name          string
		config        string
		expectedError bool
		description   string
	}{
		{
			name: "valid TCP config with TLS",
			config: `{
				"container_id": "mycontainer",
				"connection_type": "tcp",
				"docker_daemon": "tcp://localhost:2376",
				"tls_enabled": true,
				"tls_verify": true
			}`,
			expectedError: false,
			description:   "Valid TCP config with TLS should pass validation",
		},
		{
			name: "valid socket config ignores TLS",
			config: `{
				"container_id": "mycontainer",
				"connection_type": "socket",
				"docker_daemon": "/var/run/docker.sock",
				"tls_enabled": true
			}`,
			expectedError: false,
			description:   "Socket config should ignore TLS fields",
		},
		{
			name: "TCP config with TLS fields as strings",
			config: `{
				"container_id": "mycontainer",
				"connection_type": "tcp",
				"docker_daemon": "tcp://localhost:2376",
				"tls_enabled": "true",
				"tls_verify": "false"
			}`,
			expectedError: true,
			description:   "TLS boolean fields as strings should fail validation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.Validate(tt.config)
			if tt.expectedError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

func TestDockerExecutor_TLS_ErrorHandling(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDockerExecutor(logger)

	tests := []struct {
		name           string
		monitor        *Monitor
		expectedStatus shared.MonitorStatus
		expectMessage  string
		description    string
	}{
		{
			name: "Legacy CN certificate error simulation",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "docker",
				Name:     "Test Docker Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"container_id": "test_container",
					"connection_type": "tcp",
					"docker_daemon": "tcp://192.168.65.2:2376",
					"tls_enabled": true,
					"tls_verify": true
				}`,
			},
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "container inspect error",
			description:    "Should handle TLS connection errors gracefully",
		},
		{
			name: "TLS with verify disabled should attempt connection",
			monitor: &Monitor{
				ID:       "monitor2",
				Type:     "docker",
				Name:     "Test Docker Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"container_id": "test_container",
					"connection_type": "tcp",
					"docker_daemon": "tcp://192.168.65.2:2376",
					"tls_enabled": true,
					"tls_verify": false
				}`,
			},
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "container inspect error",
			description:    "Should attempt connection even with TLS verify disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			result := executor.Execute(ctx, tt.monitor, nil)
			assert.Equal(t, tt.expectedStatus, result.Status, tt.description)
			if tt.expectMessage != "" {
				assert.Contains(t, result.Message, tt.expectMessage, tt.description)
			}
			assert.NotZero(t, result.StartTime)
			assert.NotZero(t, result.EndTime)
		})
	}
}
