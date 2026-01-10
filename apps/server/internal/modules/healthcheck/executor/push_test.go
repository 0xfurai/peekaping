package executor

import (
	"context"
	"vigi/internal/modules/shared"
	"testing"
	"time"

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

	now := time.Now().UTC()

	tests := []struct {
		name           string
		monitor        *Monitor
		config         string
		expectedStatus *shared.MonitorStatus
		expectedMsg    string
	}{
		{
			name: "no heartbeat - should return DOWN",
			monitor: &Monitor{
				ID:            "monitor1",
				Type:          "push",
				Name:          "Test Monitor",
				Interval:      30,
				LastHeartbeat: nil,
			},
			config: `{
				"pushToken": "valid-token"
			}`,
			expectedStatus: func() *shared.MonitorStatus { s := shared.MonitorStatusDown; return &s }(),
			expectedMsg:    "No push received yet",
		},
		{
			name: "heartbeat within interval - should return nil",
			monitor: &Monitor{
				ID:       "monitor2",
				Type:     "push",
				Name:     "Another Monitor",
				Interval: 60,
				LastHeartbeat: &shared.HeartBeatModel{
					ID:        "hb1",
					MonitorID: "monitor2",
					Status:    shared.MonitorStatusUp,
					Time:      now.Add(-30 * time.Second), // 30 seconds ago, within 60 second interval
				},
			},
			config: `{
				"pushToken": "another-token"
			}`,
			expectedStatus: nil,
		},
		{
			name: "heartbeat outside interval - should return DOWN",
			monitor: &Monitor{
				ID:       "monitor3",
				Type:     "push",
				Name:     "Expired Monitor",
				Interval: 30,
				LastHeartbeat: &shared.HeartBeatModel{
					ID:        "hb2",
					MonitorID: "monitor3",
					Status:    shared.MonitorStatusUp,
					Time:      now.Add(-60 * time.Second), // 60 seconds ago, outside 30 second interval
				},
			},
			config: `{
				"pushToken": "expired-token"
			}`,
			expectedStatus: func() *shared.MonitorStatus { s := shared.MonitorStatusDown; return &s }(),
			expectedMsg:    "No push received in time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute
			tt.monitor.Config = tt.config
			result := executor.Execute(context.Background(), tt.monitor, nil)

			if tt.expectedStatus == nil {
				assert.Nil(t, result, "Push executor should return nil when heartbeat is within interval")
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, *tt.expectedStatus, result.Status)
				assert.Equal(t, tt.expectedMsg, result.Message)
			}
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
		LastHeartbeat: nil,
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

	// Assert that result is DOWN since no heartbeat
	assert.NotNil(t, result)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Equal(t, "No push received yet", result.Message)
}
