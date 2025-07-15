package executor

import (
	"context"
	"peekaping/src/modules/shared"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestMySQLExecutor_Validate(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMySQLExecutor(logger)

	tests := []struct {
		name        string
		configJSON  string
		expectError bool
	}{
		{
			name: "valid config",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": "SELECT 1"
			}`,
			expectError: false,
		},
		{
			name: "missing connection_string",
			configJSON: `{
				"query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "missing query",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb"
			}`,
			expectError: true,
		},
		{
			name: "empty connection_string",
			configJSON: `{
				"connection_string": "",
				"query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "empty query",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": ""
			}`,
			expectError: true,
		},
		{
			name: "invalid json",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": "SELECT 1"
			`,
			expectError: true,
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

func TestMySQLExecutor_Unmarshal(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMySQLExecutor(logger)

	configJSON := `{
		"connection_string": "mysql://user:password@localhost:3306/testdb",
		"query": "SELECT 1"
	}`

	result, err := executor.Unmarshal(configJSON)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	cfg := result.(*MySQLConfig)
	assert.Equal(t, "mysql://user:password@localhost:3306/testdb", cfg.ConnectionString)
	assert.Equal(t, "SELECT 1", cfg.Query)
}

func TestMySQLExecutor_Execute(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMySQLExecutor(logger)

	monitor := &Monitor{
		ID:       "test-monitor",
		Name:     "Test MySQL Monitor",
		Type:     "mysql",
		Interval: 60,
		Timeout:  30,
		Config: `{
			"connection_string": "mysql://user:password@localhost:3306/testdb",
			"query": "SELECT 1"
		}`,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This test will fail if there's no MySQL server running, but it validates the structure
	result := executor.Execute(ctx, monitor, nil)
	assert.NotNil(t, result)
	assert.NotEmpty(t, result.Message)
	assert.False(t, result.StartTime.IsZero())
	assert.False(t, result.EndTime.IsZero())
	assert.True(t, result.EndTime.After(result.StartTime) || result.EndTime.Equal(result.StartTime))
}

func TestMySQLExecutor_ExecuteWithInvalidConfig(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMySQLExecutor(logger)

	monitor := &Monitor{
		ID:       "test-monitor",
		Name:     "Test MySQL Monitor",
		Type:     "mysql",
		Interval: 60,
		Timeout:  30,
		Config:   `{"invalid": "config"}`,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := executor.Execute(ctx, monitor, nil)
	assert.NotNil(t, result)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "failed to parse config")
}

func TestMySQLExecutor_parseMySQLURL(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMySQLExecutor(logger)

	tests := []struct {
		name          string
		connectionURL string
		expectedDSN   string
		expectError   bool
	}{
		{
			name:          "valid URL with password in URL",
			connectionURL: "mysql://user:password@localhost:3306/testdb",
			expectedDSN:   "user:password@tcp(localhost:3306)/testdb",
			expectError:   false,
		},
		{
			name:          "valid URL without password",
			connectionURL: "mysql://user@localhost:3306/testdb",
			expectedDSN:   "user:@tcp(localhost:3306)/testdb",
			expectError:   false,
		},
		{
			name:          "valid URL with query parameters",
			connectionURL: "mysql://user:password@localhost:3306/testdb?charset=utf8",
			expectedDSN:   "user:password@tcp(localhost:3306)/testdb?charset=utf8",
			expectError:   false,
		},
		{
			name:          "valid URL with default port",
			connectionURL: "mysql://user:password@localhost/testdb",
			expectedDSN:   "user:password@tcp(localhost:3306)/testdb",
			expectError:   false,
		},
		{
			name:          "invalid scheme",
			connectionURL: "postgres://user:password@localhost:5432/testdb",
			expectedDSN:   "",
			expectError:   true,
		},
		{
			name:          "missing database",
			connectionURL: "mysql://user:password@localhost:3306",
			expectedDSN:   "",
			expectError:   true,
		},
		{
			name:          "invalid URL format",
			connectionURL: "not-a-valid-url",
			expectedDSN:   "",
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, err := executor.parseMySQLURL(tt.connectionURL)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedDSN, dsn)
			}
		})
	}
}
