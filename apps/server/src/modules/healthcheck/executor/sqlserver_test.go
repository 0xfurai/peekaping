package executor

import (
	"context"
	"peekaping/src/modules/shared"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestSQLServerExecutor_Validate(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewSQLServerExecutor(logger)

	tests := []struct {
		name        string
		configJSON  string
		expectError bool
	}{
		{
			name: "valid config with semicolon format",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password=TestPassword123!;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30",
				"database_query": "SELECT 1"
			}`,
			expectError: false,
		},
		{
			name: "valid config without port",
			configJSON: `{
				"database_connection_string": "Server=localhost;Database=master;User Id=sa;Password=TestPassword123!;Encrypt=false;TrustServerCertificate=true",
				"database_query": "SELECT 1"
			}`,
			expectError: false,
		},
		{
			name: "valid config with minimal parameters",
			configJSON: `{
				"database_connection_string": "Server=localhost;Database=master;User Id=sa;Password=password",
				"database_query": "SELECT 1"
			}`,
			expectError: false,
		},
		{
			name: "valid config with boolean variations",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password=password;Encrypt=yes;TrustServerCertificate=no",
				"database_query": "SELECT TOP 1 * FROM INFORMATION_SCHEMA.TABLES"
			}`,
			expectError: false,
		},
		{
			name: "valid config with SHOW statement",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password=password",
				"database_query": "SHOW TABLES"
			}`,
			expectError: false,
		},
		{
			name: "valid config with DESCRIBE statement",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password=password",
				"database_query": "DESCRIBE users"
			}`,
			expectError: false,
		},
		{
			name: "valid config with EXPLAIN statement",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password=password",
				"database_query": "EXPLAIN SELECT * FROM users"
			}`,
			expectError: false,
		},
		{
			name: "valid config with WITH statement (CTE)",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password=password",
				"database_query": "WITH UserCTE AS (SELECT * FROM users) SELECT COUNT(*) FROM UserCTE"
			}`,
			expectError: false,
		},
		{
			name: "valid config with VALUES statement",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password=password",
				"database_query": "VALUES (1, 'test')"
			}`,
			expectError: false,
		},
		{
			name: "valid config with empty query",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password=password",
				"database_query": ""
			}`,
			expectError: false,
		},
		{
			name: "valid config without query",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password=password"
			}`,
			expectError: false,
		},
		{
			name: "backward compatibility - legacy URL format",
			configJSON: `{
				"database_connection_string": "sqlserver://sa:password@localhost:1433?database=master",
				"database_query": "SELECT 1"
			}`,
			expectError: false,
		},
		{
			name: "backward compatibility - mssql scheme",
			configJSON: `{
				"database_connection_string": "mssql://user:password@server:1433?database=testdb",
				"database_query": "SELECT TOP 1 * FROM INFORMATION_SCHEMA.TABLES"
			}`,
			expectError: false,
		},
		{
			name: "valid config with case-insensitive parameters",
			configJSON: `{
				"database_connection_string": "server=localhost,1433;database=master;user id=sa;password=TestPassword123!;encrypt=false;trustservercertificate=true;connection timeout=30",
				"database_query": "SELECT 1"
			}`,
			expectError: false,
		},
		{
			name: "valid config with mixed case parameters",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;database=master;User Id=sa;PASSWORD=TestPassword123!;Encrypt=false;TrustServerCertificate=true",
				"database_query": "SELECT 1"
			}`,
			expectError: false,
		},
		{
			name: "missing connection string",
			configJSON: `{
				"database_query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "empty connection string",
			configJSON: `{
				"database_connection_string": "",
				"database_query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "missing Server parameter",
			configJSON: `{
				"database_connection_string": "Database=master;User Id=sa;Password=password",
				"database_query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "missing Database parameter",
			configJSON: `{
				"database_connection_string": "Server=localhost;User Id=sa;Password=password",
				"database_query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "missing User Id parameter",
			configJSON: `{
				"database_connection_string": "Server=localhost;Database=master;Password=password",
				"database_query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "invalid port number",
			configJSON: `{
				"database_connection_string": "Server=localhost,abc;Database=master;User Id=sa;Password=password",
				"database_query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "port out of range",
			configJSON: `{
				"database_connection_string": "Server=localhost,99999;Database=master;User Id=sa;Password=password",
				"database_query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "invalid Encrypt value",
			configJSON: `{
				"database_connection_string": "Server=localhost;Database=master;User Id=sa;Password=password;Encrypt=maybe",
				"database_query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "invalid TrustServerCertificate value",
			configJSON: `{
				"database_connection_string": "Server=localhost;Database=master;User Id=sa;Password=password;TrustServerCertificate=maybe",
				"database_query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "invalid Connection Timeout value",
			configJSON: `{
				"database_connection_string": "Server=localhost;Database=master;User Id=sa;Password=password;Connection Timeout=abc",
				"database_query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "invalid query - delete statement",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password=password",
				"database_query": "DELETE FROM users"
			}`,
			expectError: true,
		},
		{
			name: "invalid query - insert statement",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password=password",
				"database_query": "INSERT INTO users VALUES (1, 'test')"
			}`,
			expectError: true,
		},
		{
			name: "invalid query - update statement",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password=password",
				"database_query": "UPDATE users SET name = 'test'"
			}`,
			expectError: true,
		},
		{
			name: "invalid query - drop statement",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password=password",
				"database_query": "DROP TABLE users"
			}`,
			expectError: true,
		},
		{
			name: "invalid connection string format",
			configJSON: `{
				"database_connection_string": "mysql://user:password@localhost:3306/testdb",
				"database_query": "SELECT 1"
			}`,
			expectError: true,
		},
		{
			name: "malformed JSON",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password=password"
				"database_query": "SELECT 1"
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

func TestSQLServerExecutor_Unmarshal(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewSQLServerExecutor(logger)

	tests := []struct {
		name       string
		configJSON string
		wantError  bool
		wantConfig *SQLServerConfig
	}{
		{
			name: "valid config",
			configJSON: `{
				"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password=password",
				"database_query": "SELECT 1"
			}`,
			wantError: false,
			wantConfig: &SQLServerConfig{
				DatabaseConnectionString: "Server=localhost,1433;Database=master;User Id=sa;Password=password",
				DatabaseQuery:            "SELECT 1",
			},
		},
		{
			name: "minimal config",
			configJSON: `{
				"database_connection_string": "Server=localhost;Database=master;User Id=sa;Password=password"
			}`,
			wantError: false,
			wantConfig: &SQLServerConfig{
				DatabaseConnectionString: "Server=localhost;Database=master;User Id=sa;Password=password",
				DatabaseQuery:            "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Unmarshal(tt.configJSON)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				config, ok := result.(*SQLServerConfig)
				assert.True(t, ok)
				assert.Equal(t, tt.wantConfig, config)
			}
		})
	}
}

func TestSQLServerExecutor_parseConnectionString(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewSQLServerExecutor(logger)

	tests := []struct {
		name          string
		connectionStr string
		expectedDSN   string
		expectError   bool
	}{
		{
			name:          "semicolon format - basic",
			connectionStr: "Server=localhost,1433;Database=master;User Id=sa;Password=TestPassword123!;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30",
			expectedDSN:   "Server=localhost,1433;Database=master;User Id=sa;Password=TestPassword123!;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30",
			expectError:   false,
		},
		{
			name:          "semicolon format - minimal",
			connectionStr: "Server=localhost;Database=master;User Id=sa;Password=password",
			expectedDSN:   "Server=localhost;Database=master;User Id=sa;Password=password",
			expectError:   false,
		},
		{
			name:          "legacy URL format - basic sqlserver",
			connectionStr: "sqlserver://sa:password@localhost:1433?database=master",
			expectedDSN:   "Server=localhost,1433;User Id=sa;Password=password;Database=master",
			expectError:   false,
		},
		{
			name:          "legacy URL format - mssql scheme",
			connectionStr: "mssql://user:pass@server:1433?database=testdb",
			expectedDSN:   "Server=server,1433;User Id=user;Password=pass;Database=testdb",
			expectError:   false,
		},
		{
			name:          "legacy URL format - default port",
			connectionStr: "sqlserver://sa:password@localhost?database=master",
			expectedDSN:   "Server=localhost;User Id=sa;Password=password;Database=master",
			expectError:   false,
		},
		{
			name:          "legacy URL format - with additional parameters",
			connectionStr: "sqlserver://user:pass@server:1433?database=testdb&encrypt=disable&trustServerCertificate=true",
			expectedDSN:   "Server=server,1433;User Id=user;Password=pass;Database=testdb;Encrypt=disable;TrustServerCertificate=true",
			expectError:   false,
		},
		{
			name:          "invalid format",
			connectionStr: "postgres://user:pass@server:5432/database",
			expectedDSN:   "",
			expectError:   true,
		},
		{
			name:          "malformed URL",
			connectionStr: "not-a-url",
			expectedDSN:   "",
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, err := executor.parseConnectionString(tt.connectionStr)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// For semicolon format, expect exact match since it's a passthrough
				if strings.HasPrefix(tt.connectionStr, "Server=") {
					assert.Equal(t, tt.expectedDSN, dsn)
				} else {
					// For legacy URL format, check that all expected parameters are present
					// since Go map iteration order is not guaranteed
					expectedPairs := strings.Split(tt.expectedDSN, ";")
					actualPairs := strings.Split(dsn, ";")

					assert.Equal(t, len(expectedPairs), len(actualPairs), "Number of parameters should match")

					// Convert to maps for comparison
					expectedMap := make(map[string]string)
					for _, pair := range expectedPairs {
						if idx := strings.Index(pair, "="); idx > 0 {
							key := pair[:idx]
							value := pair[idx+1:]
							expectedMap[key] = value
						}
					}

					actualMap := make(map[string]string)
					for _, pair := range actualPairs {
						if idx := strings.Index(pair, "="); idx > 0 {
							key := pair[:idx]
							value := pair[idx+1:]
							actualMap[key] = value
						}
					}

					assert.Equal(t, expectedMap, actualMap, "Parameter key-value pairs should match")
				}
			}
		})
	}
}

func TestSQLServerExecutor_validateQuery(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewSQLServerExecutor(logger)

	tests := []struct {
		name        string
		query       string
		expectError bool
	}{
		{
			name:        "valid SELECT query",
			query:       "SELECT 1",
			expectError: false,
		},
		{
			name:        "valid SHOW query",
			query:       "SHOW TABLES",
			expectError: false,
		},
		{
			name:        "valid DESCRIBE query",
			query:       "DESCRIBE users",
			expectError: false,
		},
		{
			name:        "valid DESC query",
			query:       "DESC users",
			expectError: false,
		},
		{
			name:        "valid EXPLAIN query",
			query:       "EXPLAIN SELECT * FROM users",
			expectError: false,
		},
		{
			name:        "valid WITH query",
			query:       "WITH cte AS (SELECT 1) SELECT * FROM cte",
			expectError: false,
		},
		{
			name:        "valid VALUES query",
			query:       "VALUES (1, 'test')",
			expectError: false,
		},
		{
			name:        "empty query",
			query:       "",
			expectError: false,
		},
		{
			name:        "whitespace only query",
			query:       "   \t\n   ",
			expectError: false,
		},
		{
			name:        "case insensitive SELECT",
			query:       "select * from users",
			expectError: false,
		},
		{
			name:        "invalid DELETE query",
			query:       "DELETE FROM users",
			expectError: true,
		},
		{
			name:        "invalid INSERT query",
			query:       "INSERT INTO users VALUES (1, 'test')",
			expectError: true,
		},
		{
			name:        "invalid UPDATE query",
			query:       "UPDATE users SET name = 'test'",
			expectError: true,
		},
		{
			name:        "invalid DROP query",
			query:       "DROP TABLE users",
			expectError: true,
		},
		{
			name:        "invalid CREATE query",
			query:       "CREATE TABLE test (id INT)",
			expectError: true,
		},
		{
			name:        "invalid ALTER query",
			query:       "ALTER TABLE users ADD COLUMN test VARCHAR(50)",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.validateQuery(tt.query)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSQLServerExecutor_Execute(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewSQLServerExecutor(logger)

	// Test with invalid config
	monitor := &Monitor{
		Name:    "test-monitor",
		Timeout: 10,
		Config:  `{"invalid": "config"}`,
	}

	result := executor.Execute(context.Background(), monitor, nil)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "failed to parse config")

	// Test with valid config but invalid connection string
	monitor.Config = `{
		"database_connection_string": "invalid-connection-string",
		"database_query": "SELECT 1"
	}`

	result = executor.Execute(context.Background(), monitor, nil)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "connection string validation failed")
}
