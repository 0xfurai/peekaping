package executor

import (
	"context"
	"peekaping/src/modules/shared"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestDNSExecutor_Unmarshal(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDNSExecutor(logger)

	tests := []struct {
		name          string
		config        string
		expectedError bool
		expectedHost  string
		expectedPort  int
	}{
		{
			name: "valid config",
			config: `{
				"host": "example.com",
				"resolver_server": "8.8.8.8",
				"port": 53,
				"resolve_type": "A"
			}`,
			expectedError: false,
			expectedHost:  "example.com",
			expectedPort:  53,
		},
		{
			name: "invalid json",
			config: `{
				"host": "example.com",
				invalid
			}`,
			expectedError: true,
		},
		{
			name:          "empty config",
			config:        `{}`,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Unmarshal(tt.config)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.expectedHost != "" {
					cfg := result.(*DNSConfig)
					assert.Equal(t, tt.expectedHost, cfg.Host)
					assert.Equal(t, tt.expectedPort, cfg.Port)
				}
			}
		})
	}
}

func TestDNSExecutor_Validate(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDNSExecutor(logger)

	tests := []struct {
		name          string
		config        string
		expectedError bool
	}{
		{
			name: "valid config with all fields",
			config: `{
				"host": "example.com",
				"resolver_server": "8.8.8.8",
				"port": 53,
				"resolve_type": "A"
			}`,
			expectedError: false,
		},
		{
			name: "missing required host",
			config: `{
				"resolver_server": "8.8.8.8",
				"port": 53,
				"resolve_type": "A"
			}`,
			expectedError: true,
		},
		{
			name: "missing required resolver_server",
			config: `{
				"host": "example.com",
				"port": 53,
				"resolve_type": "A"
			}`,
			expectedError: true,
		},
		{
			name: "invalid resolver_server (not IP)",
			config: `{
				"host": "example.com",
				"resolver_server": "not-an-ip",
				"port": 53,
				"resolve_type": "A"
			}`,
			expectedError: true,
		},
		{
			name: "invalid port (too high)",
			config: `{
				"host": "example.com",
				"resolver_server": "8.8.8.8",
				"port": 70000,
				"resolve_type": "A"
			}`,
			expectedError: true,
		},
		{
			name: "invalid port (zero)",
			config: `{
				"host": "example.com",
				"resolver_server": "8.8.8.8",
				"port": 0,
				"resolve_type": "A"
			}`,
			expectedError: true,
		},
		{
			name: "invalid resolve_type",
			config: `{
				"host": "example.com",
				"resolver_server": "8.8.8.8",
				"port": 53,
				"resolve_type": "INVALID"
			}`,
			expectedError: true,
		},
		{
			name: "valid with AAAA type",
			config: `{
				"host": "example.com",
				"resolver_server": "1.1.1.1",
				"port": 53,
				"resolve_type": "AAAA"
			}`,
			expectedError: false,
		},
		{
			name: "valid with MX type",
			config: `{
				"host": "example.com",
				"resolver_server": "8.8.4.4",
				"port": 53,
				"resolve_type": "MX"
			}`,
			expectedError: false,
		},
		{
			name: "valid with alternative port",
			config: `{
				"host": "example.com",
				"resolver_server": "8.8.8.8",
				"port": 5353,
				"resolve_type": "A"
			}`,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.Validate(tt.config)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDNSExecutor_Execute(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDNSExecutor(logger)

	tests := []struct {
		name           string
		monitor        *Monitor
		expectedStatus shared.MonitorStatus
		expectMessage  string
	}{
		{
			name: "successful A record lookup",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "dns",
				Name:     "Test DNS Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"host": "google.com",
					"resolver_server": "8.8.8.8",
					"port": 53,
					"resolve_type": "A"
				}`,
			},
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "A records:",
		},
		{
			name: "failed lookup with non-existent domain",
			monitor: &Monitor{
				ID:       "monitor2",
				Type:     "dns",
				Name:     "Test DNS Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"host": "this-domain-definitely-does-not-exist-12345.com",
					"resolver_server": "8.8.8.8",
					"port": 53,
					"resolve_type": "A"
				}`,
			},
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "DNS lookup failed",
		},
		{
			name: "invalid config",
			monitor: &Monitor{
				ID:       "monitor3",
				Type:     "dns",
				Name:     "Test DNS Monitor",
				Interval: 30,
				Timeout:  5,
				Config:   `{invalid json}`,
			},
			expectedStatus: shared.MonitorStatusDown,
		},
		{
			name: "timeout with very short timeout",
			monitor: &Monitor{
				ID:       "monitor4",
				Type:     "dns",
				Name:     "Test DNS Monitor",
				Interval: 30,
				Timeout:  1, // Very short timeout
				Config: `{
					"host": "google.com",
					"resolver_server": "1.2.3.4",
					"port": 53,
					"resolve_type": "A"
				}`,
			},
			expectedStatus: shared.MonitorStatusDown,
		},
		{
			name: "successful CNAME lookup",
			monitor: &Monitor{
				ID:       "monitor5",
				Type:     "dns",
				Name:     "Test DNS Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"host": "www.google.com",
					"resolver_server": "8.8.8.8",
					"port": 53,
					"resolve_type": "CNAME"
				}`,
			},
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "CNAME:",
		},
		{
			name: "successful CAA record lookup",
			monitor: &Monitor{
				ID:       "monitor6",
				Type:     "dns",
				Name:     "Test DNS Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"host": "cloudflare.com",
					"resolver_server": "8.8.8.8",
					"port": 53,
					"resolve_type": "CAA"
				}`,
			},
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "CAA records:",
		},
		{
			name: "successful SOA record lookup",
			monitor: &Monitor{
				ID:       "monitor7",
				Type:     "dns",
				Name:     "Test DNS Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"host": "example.com",
					"resolver_server": "8.8.8.8",
					"port": 53,
					"resolve_type": "SOA"
				}`,
			},
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "SOA:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			result := executor.Execute(ctx, tt.monitor, nil)
			assert.Equal(t, tt.expectedStatus, result.Status)
			if tt.expectMessage != "" {
				assert.Contains(t, result.Message, tt.expectMessage)
			}
		})
	}
}

func TestDNSExecutor_Execute_DifferentRecordTypes(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDNSExecutor(logger)

	// Test different record types with a known domain
	recordTypes := []string{"A", "AAAA", "MX", "NS", "TXT"}

	for _, recordType := range recordTypes {
		t.Run("record_type_"+recordType, func(t *testing.T) {
			monitor := &Monitor{
				ID:       "monitor1",
				Type:     "dns",
				Name:     "Test DNS Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"host": "google.com",
					"resolver_server": "8.8.8.8",
					"port": 53,
					"resolve_type": "` + recordType + `"
				}`,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			result := executor.Execute(ctx, monitor, nil)
			// Most of these should succeed for google.com
			assert.NotNil(t, result)
			assert.NotEmpty(t, result.Message)
		})
	}
}

func TestDNSExecutor_Execute_WithProxy(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDNSExecutor(logger)

	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "dns",
		Name:     "Test DNS Monitor",
		Interval: 30,
		Timeout:  5,
		Config: `{
			"host": "example.com",
			"resolver_server": "8.8.8.8",
			"port": 53,
			"resolve_type": "A"
		}`,
	}

	// DNS executor should ignore proxy
	proxy := &Proxy{
		ID:       "proxy1",
		Host:     "proxy.example.com",
		Port:     8080,
		Protocol: "http",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := executor.Execute(ctx, monitor, proxy)
	assert.NotNil(t, result)
	// Proxy should not affect DNS execution
}

func TestNewDNSExecutor(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()

	// Test executor creation
	executor := NewDNSExecutor(logger)

	// Verify executor is properly initialized
	assert.NotNil(t, executor)
	assert.NotNil(t, executor.logger)
}
