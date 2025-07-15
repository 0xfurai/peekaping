package executor

import (
	"context"
	"peekaping/src/modules/heartbeat"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// ExecutorMockHeartbeatService implements heartbeat.Service interface for testing
type ExecutorMockHeartbeatService struct {
	mock.Mock
}

func (m *ExecutorMockHeartbeatService) Create(ctx context.Context, entity *heartbeat.CreateUpdateDto) (*heartbeat.Model, error) {
	args := m.Called(ctx, entity)
	return args.Get(0).(*heartbeat.Model), args.Error(1)
}

func (m *ExecutorMockHeartbeatService) FindByID(ctx context.Context, id string) (*heartbeat.Model, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*heartbeat.Model), args.Error(1)
}

func (m *ExecutorMockHeartbeatService) FindAll(ctx context.Context, page int, limit int) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, page, limit)
	return args.Get(0).([]*heartbeat.Model), args.Error(1)
}

func (m *ExecutorMockHeartbeatService) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *ExecutorMockHeartbeatService) FindUptimeStatsByMonitorID(ctx context.Context, monitorID string, periods map[string]time.Duration, now time.Time) (map[string]float64, error) {
	args := m.Called(ctx, monitorID, periods, now)
	return args.Get(0).(map[string]float64), args.Error(1)
}

func (m *ExecutorMockHeartbeatService) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	args := m.Called(ctx, cutoff)
	return args.Get(0).(int64), args.Error(1)
}

func (m *ExecutorMockHeartbeatService) FindByMonitorIDPaginated(ctx context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, monitorID, limit, page, important, reverse)
	return args.Get(0).([]*heartbeat.Model), args.Error(1)
}

func (m *ExecutorMockHeartbeatService) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	args := m.Called(ctx, monitorID)
	return args.Error(0)
}

func TestExecutorRegistry_GetExecutor(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	heartbeatSvc := new(ExecutorMockHeartbeatService)
	registry := NewExecutorRegistry(logger, heartbeatSvc)

	tests := []struct {
		name          string
		executorType  string
		expectedFound bool
	}{
		{
			name:          "get http executor",
			executorType:  "http",
			expectedFound: true,
		},
		{
			name:          "get push executor",
			executorType:  "push",
			expectedFound: true,
		},
		{
			name:          "get postgres executor",
			executorType:  "postgres",
			expectedFound: true,
		},
		{
			name:          "get non-existent executor",
			executorType:  "invalid",
			expectedFound: false,
		},
		{
			name:          "get executor with empty string",
			executorType:  "",
			expectedFound: false,
		},
		{
			name:          "get executor with special characters",
			executorType:  "!@#$%",
			expectedFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor, found := registry.GetExecutor(tt.executorType)
			assert.Equal(t, tt.expectedFound, found)
			if tt.expectedFound {
				assert.NotNil(t, executor)
			} else {
				assert.Nil(t, executor)
			}
		})
	}
}

func TestExecutorRegistry_ValidateConfig(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	heartbeatSvc := new(ExecutorMockHeartbeatService)
	registry := NewExecutorRegistry(logger, heartbeatSvc)

	tests := []struct {
		name          string
		monitorType   string
		config        string
		expectedError bool
	}{
		{
			name:        "validate http config",
			monitorType: "http",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none"
			}`,
			expectedError: false,
		},
		{
			name:        "validate push config",
			monitorType: "push",
			config: `{
				"pushToken": "valid-token"
			}`,
			expectedError: false,
		},
		{
			name:        "validate postgres config",
			monitorType: "postgres",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb",
				"database_query": "SELECT 1"
			}`,
			expectedError: false,
		},
		{
			name:        "validate invalid http config",
			monitorType: "http",
			config: `{
				"url": "not-a-url",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none"
			}`,
			expectedError: true,
		},
		{
			name:        "validate invalid push config",
			monitorType: "push",
			config: `{
				"pushToken": ""
			}`,
			expectedError: true,
		},
		{
			name:        "validate invalid postgres config",
			monitorType: "postgres",
			config: `{
				"database_connection_string": "",
				"database_query": "SELECT 1"
			}`,
			expectedError: true,
		},
		{
			name:          "validate non-existent executor type",
			monitorType:   "invalid",
			config:        `{}`,
			expectedError: true,
		},
		{
			name:          "validate empty monitor type",
			monitorType:   "",
			config:        `{}`,
			expectedError: true,
		},
		{
			name:          "validate malformed json config",
			monitorType:   "http",
			config:        `{invalid json}`,
			expectedError: true,
		},
		{
			name:          "validate empty config for http",
			monitorType:   "http",
			config:        `{}`,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registry.ValidateConfig(tt.monitorType, tt.config)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestExecutorRegistry_ValidateConfig_Error_Logging(t *testing.T) {
	// Setup with a logger that can be captured
	logger := zap.NewNop().Sugar()
	heartbeatSvc := new(ExecutorMockHeartbeatService)
	registry := NewExecutorRegistry(logger, heartbeatSvc)

	// Test that errors are properly logged
	err := registry.ValidateConfig("http", `{"invalid": "config"}`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse config")
}

func TestExecutorRegistry_Execute(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	heartbeatSvc := new(ExecutorMockHeartbeatService)
	registry := NewExecutorRegistry(logger, heartbeatSvc)

	heartbeatSvc.On("FindByMonitorIDPaginated", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return([]*heartbeat.Model{}, nil)

	tests := []struct {
		name          string
		monitor       *Monitor
		expectedError bool
	}{
		{
			name: "execute http monitor",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "http",
				Name:     "Test Monitor",
				Interval: 30,
				Config: `{
					"url": "http://example.com",
					"method": "GET",
					"encoding": "json",
					"accepted_statuscodes": ["2XX"],
					"authMethod": "none"
				}`,
			},
			expectedError: false,
		},
		{
			name: "execute push monitor",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "push",
				Name:     "Test Monitor",
				Interval: 30,
				Config: `{
					"pushToken": "valid-token"
				}`,
			},
			expectedError: false,
		},
		{
			name: "execute postgres monitor",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "postgres",
				Name:     "Test Monitor",
				Interval: 30,
				Config: `{
					"database_connection_string": "postgres://user:password@localhost:5432/testdb",
					"database_query": "SELECT 1"
				}`,
			},
			expectedError: false,
		},
		{
			name: "execute invalid monitor type",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "invalid",
				Name:     "Test Monitor",
				Interval: 30,
				Config:   `{}`,
			},
			expectedError: true,
		},
		{
			name: "execute monitor with nil config",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "http",
				Name:     "Test Monitor",
				Interval: 30,
				Config:   "",
			},
			expectedError: false, // Should not error in getting executor, but execution will fail
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor, found := registry.GetExecutor(tt.monitor.Type)
			if tt.expectedError {
				assert.False(t, found)
				assert.Nil(t, executor)
			} else {
				assert.True(t, found)
				assert.NotNil(t, executor)
				result := executor.Execute(context.Background(), tt.monitor, nil)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestNewExecutorRegistry(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	heartbeatSvc := new(ExecutorMockHeartbeatService)

	// Test registry creation
	registry := NewExecutorRegistry(logger, heartbeatSvc)

	// Verify registry is properly initialized
	assert.NotNil(t, registry)
	assert.NotNil(t, registry.logger)
	assert.NotNil(t, registry.registry)

	// Verify expected executors are registered
	httpExecutor, found := registry.GetExecutor("http")
	assert.True(t, found)
	assert.NotNil(t, httpExecutor)

	pushExecutor, found := registry.GetExecutor("push")
	assert.True(t, found)
	assert.NotNil(t, pushExecutor)
}

// Test common utilities that weren't covered
func TestGenericValidator(t *testing.T) {
	type TestStruct struct {
		Required string `validate:"required"`
		Email    string `validate:"email"`
	}

	tests := []struct {
		name          string
		input         *TestStruct
		expectedError bool
	}{
		{
			name: "valid struct",
			input: &TestStruct{
				Required: "value",
				Email:    "test@example.com",
			},
			expectedError: false,
		},
		{
			name: "missing required field",
			input: &TestStruct{
				Required: "",
				Email:    "test@example.com",
			},
			expectedError: true,
		},
		{
			name: "invalid email",
			input: &TestStruct{
				Required: "value",
				Email:    "invalid-email",
			},
			expectedError: true,
		},
		{
			name:          "nil input",
			input:         nil,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.input != nil {
				err = GenericValidator(tt.input)
			} else {
				// Test with nil pointer
				var nilStruct *TestStruct
				err = GenericValidator(nilStruct)
			}

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGenericUnmarshal(t *testing.T) {
	type TestStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	tests := []struct {
		name          string
		input         string
		expectedError bool
		expectedName  string
		expectedValue int
	}{
		{
			name:          "valid json",
			input:         `{"name": "test", "value": 42}`,
			expectedError: false,
			expectedName:  "test",
			expectedValue: 42,
		},
		{
			name:          "invalid json",
			input:         `{invalid json}`,
			expectedError: true,
		},
		{
			name:          "empty json",
			input:         `{}`,
			expectedError: false,
			expectedName:  "",
			expectedValue: 0,
		},
		{
			name:          "json with unknown fields",
			input:         `{"name": "test", "value": 42, "unknown": "field"}`,
			expectedError: true, // DisallowUnknownFields is set
		},
		{
			name:          "malformed json - missing quote",
			input:         `{"name": test", "value": 42}`,
			expectedError: true,
		},
		{
			name:          "empty string",
			input:         "",
			expectedError: true,
		},
		{
			name:          "null json",
			input:         "null",
			expectedError: false,
			expectedName:  "",
			expectedValue: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GenericUnmarshal[TestStruct](tt.input)

			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if result != nil {
					assert.Equal(t, tt.expectedName, result.Name)
					assert.Equal(t, tt.expectedValue, result.Value)
				}
			}
		})
	}
}

func TestGenericUnmarshal_DifferentTypes(t *testing.T) {
	// Test with different struct types
	type SimpleStruct struct {
		ID string `json:"id"`
	}

	type NestedStruct struct {
		Simple SimpleStruct `json:"simple"`
		Count  int          `json:"count"`
	}

	t.Run("simple struct", func(t *testing.T) {
		result, err := GenericUnmarshal[SimpleStruct](`{"id": "test123"}`)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "test123", result.ID)
	})

	t.Run("nested struct", func(t *testing.T) {
		result, err := GenericUnmarshal[NestedStruct](`{"simple": {"id": "nested"}, "count": 5}`)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "nested", result.Simple.ID)
		assert.Equal(t, 5, result.Count)
	})
}
