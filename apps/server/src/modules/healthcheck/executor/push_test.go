package executor

import (
	"context"
	"fmt"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/shared"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// PushMockHeartbeatService implements heartbeat.Service interface for testing
type PushMockHeartbeatService struct {
	mock.Mock
}

func (m *PushMockHeartbeatService) Create(ctx context.Context, entity *heartbeat.CreateUpdateDto) (*heartbeat.Model, error) {
	args := m.Called(ctx, entity)
	return args.Get(0).(*heartbeat.Model), args.Error(1)
}

func (m *PushMockHeartbeatService) FindByID(ctx context.Context, id string) (*heartbeat.Model, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*heartbeat.Model), args.Error(1)
}

func (m *PushMockHeartbeatService) FindAll(ctx context.Context, page int, limit int) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, page, limit)
	return args.Get(0).([]*heartbeat.Model), args.Error(1)
}

func (m *PushMockHeartbeatService) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *PushMockHeartbeatService) FindUptimeStatsByMonitorID(ctx context.Context, monitorID string, periods map[string]time.Duration, now time.Time) (map[string]float64, error) {
	args := m.Called(ctx, monitorID, periods, now)
	return args.Get(0).(map[string]float64), args.Error(1)
}

func (m *PushMockHeartbeatService) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	args := m.Called(ctx, cutoff)
	return args.Get(0).(int64), args.Error(1)
}

func (m *PushMockHeartbeatService) FindByMonitorIDPaginated(ctx context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, monitorID, limit, page, important, reverse)
	return args.Get(0).([]*heartbeat.Model), args.Error(1)
}

func (m *PushMockHeartbeatService) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	args := m.Called(ctx, monitorID)
	return args.Error(0)
}

func TestPushExecutor_Validate(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	heartbeatSvc := new(PushMockHeartbeatService)
	executor := NewPushExecutor(logger, heartbeatSvc)

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
	heartbeatSvc := new(PushMockHeartbeatService)
	executor := NewPushExecutor(logger, heartbeatSvc)

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
	heartbeatSvc := new(PushMockHeartbeatService)

	// Test executor creation
	executor := NewPushExecutor(logger, heartbeatSvc)

	// Verify executor is properly initialized
	assert.NotNil(t, executor)
	assert.Equal(t, logger, executor.logger)
	assert.Equal(t, heartbeatSvc, executor.heartbeatService)
}

func TestPushExecutor_Execute(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()

	tests := []struct {
		name           string
		monitor        *Monitor
		config         string
		heartbeats     []*heartbeat.Model
		expectedStatus *shared.MonitorStatus // Use pointer to handle nil case
		expectedError  bool
		expectNil      bool // New field to indicate when nil result is expected
	}{
		{
			name: "successful push check - recent heartbeat",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: 30,
			},
			config: `{
				"pushToken": "valid-token"
			}`,
			heartbeats: []*heartbeat.Model{
				{
					ID:        "hb1",
					MonitorID: "monitor1",
					Time:      time.Now().UTC(),
					Status:    shared.MonitorStatusUp,
				},
			},
			expectedStatus: nil, // nil because function returns nil for success
			expectedError:  false,
			expectNil:      true,
		},
		{
			name: "failed push check - no heartbeat",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: 30,
			},
			config: `{
				"pushToken": "valid-token"
			}`,
			heartbeats:     []*heartbeat.Model{},
			expectedStatus: &[]shared.MonitorStatus{shared.MonitorStatusDown}[0],
			expectedError:  false,
			expectNil:      false,
		},
		{
			name: "failed push check - old heartbeat",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: 30,
			},
			config: `{
				"pushToken": "valid-token"
			}`,
			heartbeats: []*heartbeat.Model{
				{
					ID:        "hb1",
					MonitorID: "monitor1",
					Time:      time.Now().UTC().Add(-1 * time.Hour),
					Status:    shared.MonitorStatusUp,
				},
			},
			expectedStatus: &[]shared.MonitorStatus{shared.MonitorStatusDown}[0],
			expectedError:  false,
			expectNil:      false,
		},
		{
			name: "failed push check - heartbeat service error",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: 30,
			},
			config: `{
				"pushToken": "valid-token"
			}`,
			heartbeats:     nil,
			expectedStatus: &[]shared.MonitorStatus{shared.MonitorStatusDown}[0],
			expectedError:  true,
			expectNil:      false,
		},
		{
			name: "successful push check - heartbeat just within interval",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: 60, // 60 seconds
			},
			config: `{
				"pushToken": "valid-token"
			}`,
			heartbeats: []*heartbeat.Model{
				{
					ID:        "hb1",
					MonitorID: "monitor1",
					Time:      time.Now().UTC().Add(-59 * time.Second), // Just within interval
					Status:    shared.MonitorStatusUp,
				},
			},
			expectedStatus: nil,
			expectedError:  false,
			expectNil:      true,
		},
		{
			name: "failed push check - heartbeat just over interval",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: 60, // 60 seconds
			},
			config: `{
				"pushToken": "valid-token"
			}`,
			heartbeats: []*heartbeat.Model{
				{
					ID:        "hb1",
					MonitorID: "monitor1",
					Time:      time.Now().UTC().Add(-61 * time.Second),
					Status:    shared.MonitorStatusUp,
				},
			},
			expectedStatus: &[]shared.MonitorStatus{shared.MonitorStatusDown}[0],
			expectedError:  false,
			expectNil:      false,
		},
		{
			name: "push check with multiple heartbeats - latest is recent",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: 30,
			},
			config: `{
				"pushToken": "valid-token"
			}`,
			heartbeats: []*heartbeat.Model{
				{
					ID:        "hb1",
					MonitorID: "monitor1",
					Time:      time.Now().UTC().Add(-5 * time.Second), // Latest
					Status:    shared.MonitorStatusUp,
				},
				{
					ID:        "hb2",
					MonitorID: "monitor1",
					Time:      time.Now().UTC().Add(-1 * time.Hour), // Older
					Status:    shared.MonitorStatusDown,
				},
			},
			expectedStatus: nil,
			expectedError:  false,
			expectNil:      true,
		},
		{
			name: "push check with heartbeat status down but recent",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: 30,
			},
			config: `{
				"pushToken": "valid-token"
			}`,
			heartbeats: []*heartbeat.Model{
				{
					ID:        "hb1",
					MonitorID: "monitor1",
					Time:      time.Now().UTC().Add(-5 * time.Second),
					Status:    shared.MonitorStatusDown, // Status is down but push is recent
				},
			},
			expectedStatus: nil,
			expectedError:  false,
			expectNil:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh mock for each test case
			heartbeatSvc := new(PushMockHeartbeatService)
			executor := NewPushExecutor(logger, heartbeatSvc)

			// Setup mocks
			if tt.expectedError {
				heartbeatSvc.On("FindByMonitorIDPaginated", mock.Anything, tt.monitor.ID, 1, 0, (*bool)(nil), false).
					Return(([]*heartbeat.Model)(nil), fmt.Errorf("service error"))
			} else {
				heartbeatSvc.On("FindByMonitorIDPaginated", mock.Anything, tt.monitor.ID, 1, 0, (*bool)(nil), false).
					Return(tt.heartbeats, nil)
			}

			// Execute
			tt.monitor.Config = tt.config
			result := executor.Execute(context.Background(), tt.monitor, nil)

			// Assert
			if tt.expectNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				if tt.expectedStatus != nil {
					assert.Equal(t, *tt.expectedStatus, result.Status)
				}
				if tt.expectedError {
					assert.Contains(t, result.Message, "Failed to fetch heartbeat")
				}
			}

			// Verify mocks
			heartbeatSvc.AssertExpectations(t)
		})
	}
}

func TestPushExecutor_Execute_InvalidConfig(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()

	tests := []struct {
		name    string
		config  string
		monitor *Monitor
	}{
		{
			name:   "invalid JSON config",
			config: `{invalid json}`,
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: 30,
			},
		},
		{
			name:   "empty config",
			config: "",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: 30,
			},
		},
		{
			name: "config with unknown fields",
			config: `{
				"pushToken": "valid-token",
				"unknownField": "value"
			}`,
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: 30,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh mock for each test case to avoid conflicts
			heartbeatSvc := new(PushMockHeartbeatService)
			executor := NewPushExecutor(logger, heartbeatSvc)

			// Setup mock expectation since push executor will call heartbeat service even with invalid config
			heartbeatSvc.On("FindByMonitorIDPaginated", mock.Anything, tt.monitor.ID, 1, 0, (*bool)(nil), false).
				Return([]*heartbeat.Model{}, nil)

			tt.monitor.Config = tt.config

			// Execute - push executor will call heartbeat service regardless of config validity
			result := executor.Execute(context.Background(), tt.monitor, nil)

			// Should return a result (push executor always calls heartbeat service)
			assert.NotNil(t, result)
			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Equal(t, "No push received yet", result.Message)

			// Verify mock expectations
			heartbeatSvc.AssertExpectations(t)
		})
	}
}

func TestPushExecutor_Execute_WithProxy(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	heartbeatSvc := new(PushMockHeartbeatService)
	executor := NewPushExecutor(logger, heartbeatSvc)

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

	// Setup mock
	heartbeatSvc.On("FindByMonitorIDPaginated", mock.Anything, monitor.ID, 1, 0, (*bool)(nil), false).
		Return([]*heartbeat.Model{}, nil)

	// Execute with proxy
	result := executor.Execute(context.Background(), monitor, proxy)

	// Assert that proxy doesn't affect push execution
	assert.NotNil(t, result)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Equal(t, "No push received yet", result.Message)

	// Verify mocks
	heartbeatSvc.AssertExpectations(t)
}

func TestPushExecutor_Execute_HeartbeatServiceError(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	heartbeatSvc := new(PushMockHeartbeatService)
	executor := NewPushExecutor(logger, heartbeatSvc)

	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "push",
		Name:     "Test Monitor",
		Interval: 30,
		Config: `{
			"pushToken": "valid-token"
		}`,
	}

	// Setup mock to return error
	heartbeatSvc.On("FindByMonitorIDPaginated", mock.Anything, monitor.ID, 1, 0, (*bool)(nil), false).
		Return(([]*heartbeat.Model)(nil), assert.AnError)

	// Execute
	result := executor.Execute(context.Background(), monitor, nil)

	// Assert
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "Failed to fetch heartbeat")

	// Verify mocks
	heartbeatSvc.AssertExpectations(t)
}

func TestPushExecutor_Execute_EdgeCases(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()

	tests := []struct {
		name        string
		monitor     *Monitor
		config      string
		expectNil   bool
		description string
	}{
		{
			name: "zero interval monitor",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: 0, // Zero interval
			},
			config: `{
				"pushToken": "valid-token"
			}`,
			expectNil:   false,
			description: "Monitor with zero interval should always be considered down",
		},
		{
			name: "negative interval monitor",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: -30, // Negative interval
			},
			config: `{
				"pushToken": "valid-token"
			}`,
			expectNil:   false,
			description: "Monitor with negative interval should always be considered down",
		},
		{
			name: "very large interval monitor",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: 86400, // 24 hours
			},
			config: `{
				"pushToken": "valid-token"
			}`,
			expectNil:   true,
			description: "Monitor with large interval should pass if heartbeat is recent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh mock for each test case
			heartbeatSvc := new(PushMockHeartbeatService)
			executor := NewPushExecutor(logger, heartbeatSvc)

			// Create a recent heartbeat for all tests
			recentHeartbeat := []*heartbeat.Model{
				{
					ID:        "hb1",
					MonitorID: tt.monitor.ID,
					Time:      time.Now().UTC().Add(-5 * time.Second), // 5 seconds ago
					Status:    shared.MonitorStatusUp,
				},
			}

			// Setup mocks
			heartbeatSvc.On("FindByMonitorIDPaginated", mock.Anything, tt.monitor.ID, 1, 0, (*bool)(nil), false).
				Return(recentHeartbeat, nil)

			// Execute
			tt.monitor.Config = tt.config
			result := executor.Execute(context.Background(), tt.monitor, nil)

			// Assert based on expectation
			if tt.expectNil {
				assert.Nil(t, result, tt.description)
			} else {
				assert.NotNil(t, result, tt.description)
				assert.Equal(t, shared.MonitorStatusDown, result.Status, tt.description)
			}

			// Verify mocks
			heartbeatSvc.AssertExpectations(t)
		})
	}
}
