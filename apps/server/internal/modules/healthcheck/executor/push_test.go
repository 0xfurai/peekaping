package executor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestPushExecutor_Validate(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewPushExecutor(logger)

	tests := []struct {
		name          string
		config        string
		expectedError bool
	}{
		{
			name: "valid push config",
			config: `{
				"pushToken": "valid-token"
			}`,
			expectedError: false,
		},
		{
			name: "missing push token",
			config: `{
				"pushToken": ""
			}`,
			expectedError: true,
		},
		{
			name:          "empty config",
			config:        `{}`,
			expectedError: true,
		},
		{
			name: "valid push config with whitespace token",
			config: `{
				"pushToken": "  valid-token-with-spaces  "
			}`,
			expectedError: false,
		},
		{
			name: "push config with special characters in token",
			config: `{
				"pushToken": "token-with-special-chars_123!@#$%^&*()"
			}`,
			expectedError: false,
		},
		{
			name:          "malformed json",
			config:        `{invalid json}`,
			expectedError: true,
		},
		{
			name: "config with unknown fields",
			config: `{
				"pushToken": "valid-token",
				"unknownField": "value"
			}`,
			expectedError: true, // DisallowUnknownFields is set
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

func TestPushExecutor_Unmarshal(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewPushExecutor(logger)

	tests := []struct {
		name          string
		config        string
		expectedError bool
		expectedToken string
	}{
		{
			name: "valid config",
			config: `{
				"pushToken": "test-token-123"
			}`,
			expectedError: false,
			expectedToken: "test-token-123",
		},
		{
			name:          "invalid json",
			config:        `{invalid json}`,
			expectedError: true,
		},
		{
			name:          "empty string",
			config:        "",
			expectedError: true,
		},
		{
			name: "config with unknown fields",
			config: `{
				"pushToken": "test-token",
				"unknownField": "value"
			}`,
			expectedError: true, // DisallowUnknownFields is set
		},
		{
			name: "empty push token",
			config: `{
				"pushToken": ""
			}`,
			expectedError: false, // Unmarshal succeeds, validation would fail
			expectedToken: "",
		},
		{
			name:          "null json",
			config:        "null",
			expectedError: false,
			expectedToken: "",
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
				cfg, ok := result.(*PushConfig)
				assert.True(t, ok)
				assert.Equal(t, tt.expectedToken, cfg.PushToken)
			}
		})
	}
}

func TestNewPushExecutor(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()

	// Test executor creation
	executor := NewPushExecutor(logger)

	// Verify executor is properly initialized
	assert.NotNil(t, executor)
	assert.Equal(t, logger, executor.logger)
}

func TestPushExecutor_Execute(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewPushExecutor(logger)

	tests := []struct {
		name    string
		monitor *Monitor
		config  string
	}{
		{
			name: "push executor returns nil - no active check",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: 30,
			},
			config: `{
				"pushToken": "valid-token"
			}`,
		},
		{
			name: "push executor with different interval",
			monitor: &Monitor{
				ID:       "monitor2",
				Type:     "push",
				Name:     "Another Monitor",
				Interval: 60,
			},
			config: `{
				"pushToken": "another-token"
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute
			tt.monitor.Config = tt.config
			result := executor.Execute(context.Background(), tt.monitor, nil)

			// Assert - push executor is stateless and always returns nil
			// Status determination is handled by separate heartbeat monitoring
			assert.Nil(t, result, "Push executor should return nil (no-op)")
		})
	}
}

func TestPushExecutor_Execute_WithProxy(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewPushExecutor(logger)

	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "push",
		Name:     "Test Monitor",
		Interval: 30,
		Config: `{
			"pushToken": "valid-token"
		}`,
	}

	// Proxy should be ignored for push monitors
	proxy := &Proxy{
		ID:       "proxy1",
		Host:     "proxy.example.com",
		Port:     8080,
		Protocol: "http",
	}

	// Execute with proxy
	result := executor.Execute(context.Background(), monitor, proxy)

	// Assert that result is nil (stateless executor)
	assert.Nil(t, result, "Push executor should return nil regardless of proxy")
}
