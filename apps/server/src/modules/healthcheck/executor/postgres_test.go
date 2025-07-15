package executor

import (
	"context"
	"peekaping/src/modules/shared"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestPostgresExecutor_Validate(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewPostgresExecutor(logger)

	tests := []struct {
		name          string
		config        string
		expectedError bool
	}{
		{
			name: "valid config with connection string",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb",
				"database_query": "SELECT 1"
			}`,
			expectedError: false,
		},
		{
			name: "valid config with connection string only",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb"
			}`,
			expectedError: false,
		},
		{
			name: "valid config with SHOW statement",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb",
				"database_query": "SHOW TABLES"
			}`,
			expectedError: false,
		},
		{
			name: "valid config with DESCRIBE statement",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb",
				"database_query": "DESCRIBE users"
			}`,
			expectedError: false,
		},
		{
			name: "valid config with EXPLAIN statement",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb",
				"database_query": "EXPLAIN SELECT * FROM users"
			}`,
			expectedError: false,
		},
		{
			name: "valid config with WITH statement (CTE)",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb",
				"database_query": "WITH cte AS (SELECT 1) SELECT * FROM cte"
			}`,
			expectedError: false,
		},
		{
			name: "valid config with VALUES statement",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb",
				"database_query": "VALUES (1, 'test')"
			}`,
			expectedError: false,
		},
		{
			name: "invalid config - empty connection string",
			config: `{
				"database_connection_string": "",
				"database_query": "SELECT 1"
			}`,
			expectedError: true,
		},
		{
			name: "invalid config - missing connection string",
			config: `{
				"database_query": "SELECT 1"
			}`,
			expectedError: true,
		},
		{
			name: "invalid config - whitespace only query",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb",
				"database_query": "   \t\n   "
			}`,
			expectedError: true,
		},
		{
			name: "invalid config - INSERT statement",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb",
				"database_query": "INSERT INTO users VALUES (1, 'test')"
			}`,
			expectedError: true,
		},
		{
			name: "invalid config - UPDATE statement",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb",
				"database_query": "UPDATE users SET name = 'test'"
			}`,
			expectedError: true,
		},
		{
			name: "invalid config - DELETE statement",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb",
				"database_query": "DELETE FROM users"
			}`,
			expectedError: true,
		},
		{
			name: "invalid config - DROP statement",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb",
				"database_query": "DROP TABLE users"
			}`,
			expectedError: true,
		},
		{
			name: "invalid config - malformed connection string",
			config: `{
				"database_connection_string": "not-a-valid-url",
				"database_query": "SELECT 1"
			}`,
			expectedError: true,
		},
		{
			name: "invalid config - wrong scheme",
			config: `{
				"database_connection_string": "mysql://user:password@localhost:3306/testdb",
				"database_query": "SELECT 1"
			}`,
			expectedError: true,
		},
		{
			name: "invalid config - missing host",
			config: `{
				"database_connection_string": "postgres://user:password@/testdb",
				"database_query": "SELECT 1"
			}`,
			expectedError: true,
		},
		{
			name: "invalid config - missing username",
			config: `{
				"database_connection_string": "postgres://localhost:5432/testdb",
				"database_query": "SELECT 1"
			}`,
			expectedError: true,
		},
		{
			name: "invalid config - missing database name",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/",
				"database_query": "SELECT 1"
			}`,
			expectedError: true,
		},
		{
			name: "invalid config - malformed JSON",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb"
				"database_query": "SELECT 1"
			}`,
			expectedError: true,
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

func TestPostgresExecutor_Unmarshal(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewPostgresExecutor(logger)

	tests := []struct {
		name           string
		config         string
		expectedConfig *PostgresConfig
		expectedError  bool
	}{
		{
			name: "valid config",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb",
				"database_query": "SELECT 1"
			}`,
			expectedConfig: &PostgresConfig{
				DatabaseConnectionString: "postgres://user:password@localhost:5432/testdb",
				DatabaseQuery:            "SELECT 1",
			},
			expectedError: false,
		},
		{
			name: "config with only connection string",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb"
			}`,
			expectedConfig: &PostgresConfig{
				DatabaseConnectionString: "postgres://user:password@localhost:5432/testdb",
				DatabaseQuery:            "",
			},
			expectedError: false,
		},
		{
			name: "invalid JSON",
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb"
				"database_query": "SELECT 1"
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
				config := result.(*PostgresConfig)
				assert.Equal(t, tt.expectedConfig.DatabaseConnectionString, config.DatabaseConnectionString)
				assert.Equal(t, tt.expectedConfig.DatabaseQuery, config.DatabaseQuery)
			}
		})
	}
}

func TestPostgresExecutor_parseConnectionString(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewPostgresExecutor(logger)

	tests := []struct {
		name           string
		connectionStr  string
		expectedConfig map[string]string
		expectedError  bool
	}{
		{
			name:          "full connection string",
			connectionStr: "postgres://user:password@localhost:5432/testdb?sslmode=disable",
			expectedConfig: map[string]string{
				"user":     "user",
				"password": "password",
				"host":     "localhost",
				"port":     "5432",
				"dbname":   "testdb",
				"sslmode":  "disable",
			},
			expectedError: false,
		},
		{
			name:          "connection string without port",
			connectionStr: "postgres://user:password@localhost/testdb",
			expectedConfig: map[string]string{
				"user":     "user",
				"password": "password",
				"host":     "localhost",
				"port":     "5432",
				"dbname":   "testdb",
			},
			expectedError: false,
		},
		{
			name:          "connection string with postgresql scheme",
			connectionStr: "postgresql://user:password@localhost:5432/testdb",
			expectedConfig: map[string]string{
				"user":     "user",
				"password": "password",
				"host":     "localhost",
				"port":     "5432",
				"dbname":   "testdb",
			},
			expectedError: false,
		},
		{
			name:          "connection string without password",
			connectionStr: "postgres://user@localhost:5432/testdb",
			expectedConfig: map[string]string{
				"user":   "user",
				"host":   "localhost",
				"port":   "5432",
				"dbname": "testdb",
			},
			expectedError: false,
		},
		{
			name:           "malformed connection string",
			connectionStr:  "not-a-valid-url",
			expectedConfig: nil,
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.parseConnectionString(tt.connectionStr)
			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)

				// Check all expected keys are present
				for key, expectedValue := range tt.expectedConfig {
					actualValue, exists := result[key]
					assert.True(t, exists, "Expected key %s to exist", key)
					assert.Equal(t, expectedValue, actualValue, "Expected value for key %s", key)
				}
			}
		})
	}
}

func TestPostgresExecutor_Execute(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewPostgresExecutor(logger)

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
				Type:     "postgres",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
			config: `{
				"database_connection_string": ""
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectedError:  false, // No error in execution, but should return Down status
		},
		{
			name: "invalid config - dangerous query",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "postgres",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb",
				"database_query": "DROP TABLE users"
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectedError:  false,
		},
		{
			name: "invalid config - whitespace only query",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "postgres",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb",
				"database_query": "   \t\n   "
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectedError:  false,
		},
		{
			name: "malformed JSON config",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "postgres",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
			config: `{
				"database_connection_string": "postgres://user:password@localhost:5432/testdb"
				"database_query": "SELECT 1"
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectedError:  false,
		},
		{
			name: "empty password",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "postgres",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
			config: `{
				"database_connection_string": "postgres://user@localhost:5432/testdb"
			}`,
			expectedStatus: shared.MonitorStatusDown,
			expectedError:  false,
		},
		{
			name: "connection to non-existent database",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "postgres",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
			config: `{
				"database_connection_string": "postgres://user:password@nonexistent:5432/testdb"
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

func TestPostgresExecutor_validateConnectionString(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewPostgresExecutor(logger)

	tests := []struct {
		name          string
		connectionStr string
		expectedError bool
	}{
		{
			name:          "valid connection string",
			connectionStr: "postgres://user:password@localhost:5432/testdb",
			expectedError: false,
		},
		{
			name:          "valid connection string with postgresql scheme",
			connectionStr: "postgresql://user:password@localhost:5432/testdb",
			expectedError: false,
		},
		{
			name:          "empty connection string",
			connectionStr: "",
			expectedError: true,
		},
		{
			name:          "invalid scheme",
			connectionStr: "mysql://user:password@localhost:3306/testdb",
			expectedError: true,
		},
		{
			name:          "missing host",
			connectionStr: "postgres://user:password@:5432/testdb",
			expectedError: true,
		},
		{
			name:          "missing username",
			connectionStr: "postgres://localhost:5432/testdb",
			expectedError: true,
		},
		{
			name:          "missing database name",
			connectionStr: "postgres://user:password@localhost:5432/",
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

func TestPostgresExecutor_validateQuery(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewPostgresExecutor(logger)

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
			name:          "valid WITH query (CTE)",
			query:         "WITH cte AS (SELECT 1) SELECT * FROM cte",
			expectedError: false,
		},
		{
			name:          "valid VALUES query",
			query:         "VALUES (1, 'test')",
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
		{
			name:          "invalid TRUNCATE query",
			query:         "TRUNCATE TABLE users",
			expectedError: true,
		},
		{
			name:          "invalid GRANT query",
			query:         "GRANT SELECT ON users TO user",
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
