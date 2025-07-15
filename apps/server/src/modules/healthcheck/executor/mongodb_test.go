package executor

import (
	"context"
	"testing"

	"peekaping/src/modules/shared"

	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

func TestMongoDBExecutor_Unmarshal(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMongoDBExecutor(logger)

	tests := []struct {
		name        string
		configJSON  string
		expectError bool
	}{
		{
			name: "valid config",
			configJSON: `{
				"connectionString": "mongodb://localhost:27017/test",
				"command": "{\"ping\": 1}",
				"jsonPath": "$.ok",
				"expectedValue": "1"
			}`,
			expectError: false,
		},
		{
			name: "minimal config",
			configJSON: `{
				"connectionString": "mongodb://localhost:27017/test"
			}`,
			expectError: false,
		},
		{
			name: "missing connection string",
			configJSON: `{
				"command": "{\"ping\": 1}"
			}`,
			expectError: false, // Unmarshal doesn't validate required fields
		},
		{
			name:        "invalid JSON",
			configJSON:  `{"connectionString": "mongodb://localhost:27017/test",}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := executor.Unmarshal(tt.configJSON)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, cfg)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, cfg)
				mongoConfig := cfg.(*MongoDBConfig)
				assert.IsType(t, &MongoDBConfig{}, mongoConfig)
			}
		})
	}
}

func TestMongoDBExecutor_Validate(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMongoDBExecutor(logger)

	tests := []struct {
		name        string
		configJSON  string
		expectError bool
	}{
		{
			name: "valid config",
			configJSON: `{
				"connectionString": "mongodb://localhost:27017/test",
				"command": "{\"ping\": 1}",
				"jsonPath": "$.ok",
				"expectedValue": "1"
			}`,
			expectError: false,
		},
		{
			name: "missing connection string",
			configJSON: `{
				"command": "{\"ping\": 1}"
			}`,
			expectError: true, // Validate should catch required field
		},
		{
			name: "empty connection string",
			configJSON: `{
				"connectionString": "",
				"command": "{\"ping\": 1}"
			}`,
			expectError: true, // Validate should catch empty required field
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.Validate(tt.configJSON)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMongoDBExecutor_EvaluateJsonPath(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMongoDBExecutor(logger)

	// Test data
	testData := bson.M{
		"ok": 1,
		"info": bson.M{
			"version": "4.4.0",
			"status":  "healthy",
		},
		"users": []interface{}{
			bson.M{"name": "alice", "age": 30},
			bson.M{"name": "bob", "age": 25},
		},
	}

	tests := []struct {
		name          string
		path          string
		expectedValue interface{}
		expectError   bool
	}{
		{
			name:          "root path",
			path:          "$",
			expectedValue: testData,
			expectError:   false,
		},
		{
			name:          "empty path",
			path:          "",
			expectedValue: testData,
			expectError:   false,
		},
		{
			name:          "simple field",
			path:          "$.ok",
			expectedValue: 1,
			expectError:   false,
		},
		{
			name:          "nested field",
			path:          "$.info.version",
			expectedValue: "4.4.0",
			expectError:   false,
		},
		{
			name:          "nested field without $",
			path:          "info.status",
			expectedValue: "healthy",
			expectError:   false,
		},
		{
			name:        "non-existent field",
			path:        "$.nonexistent",
			expectedValue: nil,
			expectError: true,
		},
		{
			name:        "invalid nested access",
			path:        "$.ok.invalid",
			expectedValue: nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.evaluateJsonPath(testData, tt.path)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedValue, result)
			}
		})
	}
}

func TestMongoDBExecutor_SplitPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected []string
	}{
		{
			name:     "simple path",
			path:     "field",
			expected: []string{"field"},
		},
		{
			name:     "nested path",
			path:     "field.subfield",
			expected: []string{"field", "subfield"},
		},
		{
			name:     "deeply nested path",
			path:     "a.b.c.d",
			expected: []string{"a", "b", "c", "d"},
		},
		{
			name:     "escaped dot",
			path:     "field\\.with\\.dots.normal",
			expected: []string{"field.with.dots", "normal"},
		},
		{
			name:     "empty path",
			path:     "",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMongoDBExecutor_IsValueEqual(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMongoDBExecutor(logger)

	tests := []struct {
		name     string
		actual   interface{}
		expected interface{}
		result   bool
	}{
		{
			name:     "equal integers",
			actual:   1,
			expected: 1,
			result:   true,
		},
		{
			name:     "equal strings",
			actual:   "hello",
			expected: "hello",
			result:   true,
		},
		{
			name:     "integer and string representation",
			actual:   1,
			expected: "1",
			result:   true,
		},
		{
			name:     "float and integer",
			actual:   1.0,
			expected: 1,
			result:   true,
		},
		{
			name:     "different values",
			actual:   1,
			expected: 2,
			result:   false,
		},
		{
			name:     "string and number",
			actual:   "hello",
			expected: 1,
			result:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.isValueEqual(tt.actual, tt.expected)
			assert.Equal(t, tt.result, result)
		})
	}
}

func TestMongoDBExecutor_Execute_ConfigError(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMongoDBExecutor(logger)

	monitor := &Monitor{
		ID:       "test-monitor",
		Name:     "Test Monitor",
		Timeout:  5,
		Config:   `{"invalid": json}`, // Invalid JSON
	}

	result := executor.Execute(context.Background(), monitor, nil)
	assert.NotNil(t, result)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "failed to parse config")
}

func TestMongoDBExecutor_Execute_InvalidCommand(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMongoDBExecutor(logger)

	monitor := &Monitor{
		ID:      "test-monitor",
		Name:    "Test Monitor",
		Timeout: 5,
		Config: `{
			"connectionString": "mongodb://localhost:27017/test",
			"command": "invalid json"
		}`,
	}

	result := executor.Execute(context.Background(), monitor, nil)
	assert.NotNil(t, result)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "invalid MongoDB command JSON")
}

// Note: Integration tests that actually connect to MongoDB would require a running MongoDB instance
// Those tests would be better suited for integration test suites rather than unit tests