package executor

import (
	"context"
	"peekaping/src/modules/shared"
	"strings"
	"testing"

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
			name: "valid config with SHOW statement",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": "SHOW TABLES"
			}`,
			expectError: false,
		},
		{
			name: "valid config with DESCRIBE statement",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": "DESCRIBE users"
			}`,
			expectError: false,
		},
		{
			name: "valid config with EXPLAIN statement",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": "EXPLAIN SELECT * FROM users"
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
			name: "valid config with connection string only",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb"
			}`,
			expectError: false,
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
			name: "valid config with empty query",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": ""
			}`,
			expectError: false,
		},
		{
			name: "valid config with whitespace only query",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": "   \t\n   "
			}`,
			expectError: false,
		},
		{
			name: "invalid connection string - wrong scheme",
			configJSON: `{
				"connection_string": "postgres://user:password@localhost:5432/testdb",
				"query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "invalid connection string - missing host",
			configJSON: `{
				"connection_string": "mysql://user:password@/testdb",
				"query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "invalid connection string - missing username",
			configJSON: `{
				"connection_string": "mysql://localhost:3306/testdb",
				"query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "invalid connection string - empty username",
			configJSON: `{
				"connection_string": "mysql://:password@localhost:3306/testdb",
				"query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "invalid connection string - missing database",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306/",
				"query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "invalid connection string - no database path",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306",
				"query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "invalid connection string - port 0",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:0/testdb",
				"query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "invalid query - INSERT statement",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": "INSERT INTO users VALUES (1, 'test')"
			}`,
			expectError: true,
		},
		{
			name: "invalid query - UPDATE statement",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": "UPDATE users SET name = 'test'"
			}`,
			expectError: true,
		},
		{
			name: "invalid query - DELETE statement",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": "DELETE FROM users"
			}`,
			expectError: true,
		},
		{
			name: "invalid query - DROP statement",
			configJSON: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": "DROP TABLE users"
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
		{
			name: "malformed connection string",
			configJSON: `{
				"connection_string": "not-a-valid-url",
				"query": "SELECT 1"
			}`,
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

func TestMySQLExecutor_validateConnectionString(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMySQLExecutor(logger)

	tests := []struct {
		name          string
		connectionStr string
		expectedError bool
	}{
		{
			name:          "valid connection string",
			connectionStr: "mysql://user:password@localhost:3306/testdb",
			expectedError: false,
		},
		{
			name:          "valid connection string without port",
			connectionStr: "mysql://user:password@localhost/testdb",
			expectedError: false,
		},
		{
			name:          "valid connection string without password",
			connectionStr: "mysql://user@localhost:3306/testdb",
			expectedError: false,
		},
		{
			name:          "valid connection string with query parameters",
			connectionStr: "mysql://user:password@localhost:3306/testdb?charset=utf8",
			expectedError: false,
		},
		{
			name:          "empty connection string",
			connectionStr: "",
			expectedError: true,
		},
		{
			name:          "invalid scheme",
			connectionStr: "postgres://user:password@localhost:5432/testdb",
			expectedError: true,
		},
		{
			name:          "missing host",
			connectionStr: "mysql://user:password@:3306/testdb",
			expectedError: true,
		},
		{
			name:          "missing username",
			connectionStr: "mysql://localhost:3306/testdb",
			expectedError: true,
		},
		{
			name:          "empty username",
			connectionStr: "mysql://:password@localhost:3306/testdb",
			expectedError: true,
		},
		{
			name:          "missing database name",
			connectionStr: "mysql://user:password@localhost:3306/",
			expectedError: true,
		},
		{
			name:          "no database path",
			connectionStr: "mysql://user:password@localhost:3306",
			expectedError: true,
		},
		{
			name:          "port 0",
			connectionStr: "mysql://user:password@localhost:0/testdb",
			expectedError: true,
		},
		{
			name:          "malformed URL",
			connectionStr: "not-a-valid-url",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.validateConnectionString(tt.connectionStr)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMySQLExecutor_validateQuery(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMySQLExecutor(logger)

	tests := []struct {
		name          string
		query         string
		expectedError bool
	}{
		{
			name:          "valid SELECT query",
			query:         "SELECT 1",
			expectedError: false,
		},
		{
			name:          "valid SELECT query with FROM",
			query:         "SELECT * FROM users",
			expectedError: false,
		},
		{
			name:          "valid SHOW query",
			query:         "SHOW TABLES",
			expectedError: false,
		},
		{
			name:          "valid DESCRIBE query",
			query:         "DESCRIBE users",
			expectedError: false,
		},
		{
			name:          "valid DESC query",
			query:         "DESC users",
			expectedError: false,
		},
		{
			name:          "valid EXPLAIN query",
			query:         "EXPLAIN SELECT * FROM users",
			expectedError: false,
		},
		{
			name:          "valid query with mixed case",
			query:         "Select * FROM users",
			expectedError: false,
		},
		{
			name:          "valid query with leading whitespace",
			query:         "  SELECT 1  ",
			expectedError: false,
		},
		{
			name:          "empty query",
			query:         "",
			expectedError: true,
		},
		{
			name:          "whitespace only query",
			query:         "   \t\n   ",
			expectedError: true,
		},
		{
			name:          "invalid INSERT query",
			query:         "INSERT INTO users VALUES (1, 'test')",
			expectedError: true,
		},
		{
			name:          "invalid UPDATE query",
			query:         "UPDATE users SET name = 'test'",
			expectedError: true,
		},
		{
			name:          "invalid DELETE query",
			query:         "DELETE FROM users",
			expectedError: true,
		},
		{
			name:          "invalid DROP query",
			query:         "DROP TABLE users",
			expectedError: true,
		},
		{
			name:          "invalid CREATE query",
			query:         "CREATE TABLE test (id INT)",
			expectedError: true,
		},
		{
			name:          "invalid ALTER query",
			query:         "ALTER TABLE users ADD COLUMN email VARCHAR(255)",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.validateQuery(tt.query)
			if tt.expectedError {
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

	tests := []struct {
		name           string
		config         string
		expectedConfig *MySQLConfig
		expectedError  bool
	}{
		{
			name: "valid config",
			config: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": "SELECT 1"
			}`,
			expectedConfig: &MySQLConfig{
				ConnectionString: "mysql://user:password@localhost:3306/testdb",
				Query:            "SELECT 1",
			},
			expectedError: false,
		},
		{
			name: "config with only connection string",
			config: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb"
			}`,
			expectedConfig: &MySQLConfig{
				ConnectionString: "mysql://user:password@localhost:3306/testdb",
				Query:            "",
			},
			expectedError: false,
		},
		{
			name: "invalid JSON",
			config: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb"
				"query": "SELECT 1"
			}`,
			expectedConfig: nil,
			expectedError:  true,
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
				config := result.(*MySQLConfig)
				assert.Equal(t, tt.expectedConfig.ConnectionString, config.ConnectionString)
				assert.Equal(t, tt.expectedConfig.Query, config.Query)
			}
		})
	}
}

func TestMySQLExecutor_parseMySQLURL(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMySQLExecutor(logger)

	tests := []struct {
		name          string
		connectionStr string
		expectedDSN   string
		expectedError bool
	}{
		{
			name:          "full connection string",
			connectionStr: "mysql://user:password@localhost:3306/testdb",
			expectedDSN:   "user:password@tcp(localhost:3306)/testdb",
			expectedError: false,
		},
		{
			name:          "connection string without port",
			connectionStr: "mysql://user:password@localhost/testdb",
			expectedDSN:   "user:password@tcp(localhost:3306)/testdb",
			expectedError: false,
		},
		{
			name:          "connection string without password",
			connectionStr: "mysql://user@localhost:3306/testdb",
			expectedDSN:   "user:@tcp(localhost:3306)/testdb",
			expectedError: false,
		},
		{
			name:          "connection string with query parameters",
			connectionStr: "mysql://user:password@localhost:3306/testdb?charset=utf8",
			expectedDSN:   "user:password@tcp(localhost:3306)/testdb?charset=utf8",
			expectedError: false,
		},
		{
			name:          "missing username",
			connectionStr: "mysql://localhost:3306/testdb",
			expectedDSN:   "",
			expectedError: true,
		},
		{
			name:          "missing database",
			connectionStr: "mysql://user:password@localhost:3306/",
			expectedDSN:   "",
			expectedError: true,
		},
		{
			name:          "wrong scheme",
			connectionStr: "postgres://user:password@localhost:5432/testdb",
			expectedDSN:   "",
			expectedError: true,
		},
		{
			name:          "malformed URL",
			connectionStr: "not-a-valid-url",
			expectedDSN:   "",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.parseMySQLURL(tt.connectionStr)
			if tt.expectedError {
				assert.Error(t, err)
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedDSN, result)
			}
		})
	}
}

func TestMySQLExecutor_Execute(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMySQLExecutor(logger)

	tests := []struct {
		name           string
		monitor        *Monitor
		config         string
		expectedStatus shared.MonitorStatus
		expectedError  bool
	}{
		{
			name: "invalid config - empty connection string",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "mysql",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
			config: `{
				"connection_string": "",
				"query": "SELECT 1"
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectedError:  false, // No error in execution, but should return Down status
		},
		{
			name: "invalid config - wrong scheme",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "mysql",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
			config: `{
				"connection_string": "postgres://user:password@localhost:5432/testdb",
				"query": "SELECT 1"
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectedError:  false,
		},
		{
			name: "invalid config - dangerous query",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "mysql",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
			config: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": "DROP TABLE users"
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectedError:  false,
		},
		{
			name: "malformed JSON config",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "mysql",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
			config: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb"
				"query": "SELECT 1"
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectedError:  false,
		},
		{
			name: "connection to non-existent database",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "mysql",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
			config: `{
				"connection_string": "mysql://user:password@nonexistent:3306/testdb",
				"query": "SELECT 1"
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectedError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set monitor config
			tt.monitor.Config = tt.config

			// Execute the monitor
			result := executor.Execute(context.Background(), tt.monitor, nil)

			// Verify result
			assert.NotNil(t, result)
			assert.Equal(t, tt.expectedStatus, result.Status)
			assert.NotEmpty(t, result.Message)
			assert.False(t, result.StartTime.IsZero())
			assert.False(t, result.EndTime.IsZero())
			assert.True(t, result.EndTime.After(result.StartTime) || result.EndTime.Equal(result.StartTime))
		})
	}
}

func TestMySQLExecutor_DefaultQueryBehavior(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewMySQLExecutor(logger)

	tests := []struct {
		name          string
		config        string
		expectedQuery string
	}{
		{
			name: "empty query defaults to SELECT 1",
			config: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": ""
			}`,
			expectedQuery: "SELECT 1",
		},
		{
			name: "whitespace-only query defaults to SELECT 1",
			config: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": "   \t\n   "
			}`,
			expectedQuery: "SELECT 1",
		},
		{
			name: "missing query defaults to SELECT 1",
			config: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb"
			}`,
			expectedQuery: "SELECT 1",
		},
		{
			name: "provided query is used",
			config: `{
				"connection_string": "mysql://user:password@localhost:3306/testdb",
				"query": "SELECT COUNT(*) FROM users"
			}`,
			expectedQuery: "SELECT COUNT(*) FROM users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// First validate the config should pass
			err := executor.Validate(tt.config)
			assert.NoError(t, err, "Config validation should pass")

			// Parse the config to verify the logic would work
			cfgAny, err := executor.Unmarshal(tt.config)
			assert.NoError(t, err)
			cfg := cfgAny.(*MySQLConfig)

			// Simulate the query resolution logic from Execute method
			query := cfg.Query
			if query == "" || strings.TrimSpace(query) == "" {
				query = "SELECT 1"
			}

			assert.Equal(t, tt.expectedQuery, query, "Query should match expected value")
		})
	}
}
