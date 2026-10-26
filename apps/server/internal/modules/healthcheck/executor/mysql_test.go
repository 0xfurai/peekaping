package executor

import (
	"context"
	"encoding/json"
	"peekaping/internal/modules/shared"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			err := ValidateConnectionString(tt.connectionStr, []string{"mysql"})
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
		name           string
		connectionStr  string
		wantUser       string
		wantPassword   string
		wantAddr       string
		wantDatabase   string
		wantTLS        string
		wantParseTime  bool
		wantServerName string // the host name the certificate is verified against
		wantParams     map[string]string
		wantErr        string
	}{
		{
			name:          "full connection string",
			connectionStr: "mysql://user:password@localhost:3306/testdb",
			wantUser:      "user", wantPassword: "password", wantAddr: "localhost:3306", wantDatabase: "testdb",
		},
		{
			name:          "connection string without port",
			connectionStr: "mysql://user:password@localhost/testdb",
			wantUser:      "user", wantPassword: "password", wantAddr: "localhost:3306", wantDatabase: "testdb",
		},
		{
			name:          "connection string without password",
			connectionStr: "mysql://user@localhost:3306/testdb",
			wantUser:      "user", wantPassword: "", wantAddr: "localhost:3306", wantDatabase: "testdb",
		},
		{
			name:          "IPv6 host with a port",
			connectionStr: "mysql://user:password@[::1]:3306/testdb",
			wantUser:      "user", wantPassword: "password", wantAddr: "[::1]:3306", wantDatabase: "testdb",
		},
		{
			name:          "IPv6 host with another port",
			connectionStr: "mysql://user:password@[2001:db8::7]:3307/testdb",
			wantUser:      "user", wantPassword: "password", wantAddr: "[2001:db8::7]:3307", wantDatabase: "testdb",
		},
		{
			name:          "IPv6 host without a port",
			connectionStr: "mysql://user:password@[::1]/testdb",
			wantUser:      "user", wantPassword: "password", wantAddr: "[::1]:3306", wantDatabase: "testdb",
		},
		{
			name:          "percent-encoded reserved characters in the credentials",
			connectionStr: "mysql://us%3Aer%40x:p%40ss%3Aw%2Frd%251%3Fx%29@localhost:3306/testdb",
			wantUser:      "us:er@x", wantPassword: "p@ss:w/rd%1?x)", wantAddr: "localhost:3306", wantDatabase: "testdb",
		},
		{
			name:          "percent-encoded database name",
			connectionStr: "mysql://user:password@localhost:3306/test%3Fdb",
			wantUser:      "user", wantPassword: "password", wantAddr: "localhost:3306", wantDatabase: "test?db",
		},
		{
			name:          "query parameters are read by the driver",
			connectionStr: "mysql://user:password@localhost:3306/testdb?parseTime=true&tls=skip-verify",
			wantUser:      "user", wantPassword: "password", wantAddr: "localhost:3306", wantDatabase: "testdb",
			wantTLS: "skip-verify", wantParseTime: true,
		},
		{
			name:          "tls=true verifies the certificate against the host name",
			connectionStr: "mysql://u:p@db.example.com:3306/d?tls=true",
			wantUser:      "u", wantPassword: "p", wantAddr: "db.example.com:3306", wantDatabase: "d",
			wantTLS: "true", wantServerName: "db.example.com",
		},
		{
			name:          "tls=true verifies the certificate against the host name, without a port",
			connectionStr: "mysql://u:p@db.example.com/d?tls=true",
			wantUser:      "u", wantPassword: "p", wantAddr: "db.example.com:3306", wantDatabase: "d",
			wantTLS: "true", wantServerName: "db.example.com",
		},
		{
			name:          "tls=true with an IPv6 host verifies against the address without brackets",
			connectionStr: "mysql://u:p@[::1]:3306/d?tls=true",
			wantUser:      "u", wantPassword: "p", wantAddr: "[::1]:3306", wantDatabase: "d",
			wantTLS: "true", wantServerName: "::1",
		},
		{
			name:          "tls=preferred verifies nothing, so it names no server",
			connectionStr: "mysql://u:p@db.example.com:3306/d?tls=preferred",
			wantUser:      "u", wantPassword: "p", wantAddr: "db.example.com:3306", wantDatabase: "d",
			wantTLS: "preferred",
		},
		{
			name:          "a query parameter with a slash in it",
			connectionStr: "mysql://u:p@localhost:3306/d?connectionAttributes=a/b:c",
			wantUser:      "u", wantPassword: "p", wantAddr: "localhost:3306", wantDatabase: "d",
		},
		{
			name:          "a query parameter can't change the address",
			connectionStr: "mysql://user:password@localhost:3306/testdb?addr=elsewhere:1&net=unix",
			wantUser:      "user", wantPassword: "password", wantAddr: "localhost:3306", wantDatabase: "testdb",
			wantParams: map[string]string{"addr": "elsewhere:1", "net": "unix"},
		},
		{
			name:          "unknown tls setting",
			connectionStr: "mysql://user:password@localhost:3306/testdb?tls=sometimes",
			wantErr:       "invalid value / unknown config name: sometimes",
		},
		{
			name:          "missing username",
			connectionStr: "mysql://localhost:3306/testdb",
			wantErr:       "username is required in connection string",
		},
		{
			name:          "missing database",
			connectionStr: "mysql://user:password@localhost:3306/",
			wantErr:       "database name is required in connection string",
		},
		{
			name:          "wrong scheme",
			connectionStr: "postgres://user:password@localhost:5432/testdb",
			wantErr:       "connection string must use mysql:// scheme, got: postgres",
		},
		{
			name:          "malformed URL",
			connectionStr: "not-a-valid-url",
			wantErr:       "connection string must use mysql:// scheme, got: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := executor.parseMySQLURL(tt.connectionStr)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, cfg)
				return
			}
			require.NoError(t, err)

			assert.Equal(t, "tcp", cfg.Net)
			assert.Equal(t, tt.wantUser, cfg.User)
			assert.Equal(t, tt.wantPassword, cfg.Passwd)
			assert.Equal(t, tt.wantAddr, cfg.Addr)
			assert.Equal(t, tt.wantDatabase, cfg.DBName)
			assert.Equal(t, tt.wantTLS, cfg.TLSConfig)
			assert.Equal(t, tt.wantParseTime, cfg.ParseTime)
			if tt.wantServerName != "" {
				require.NotNil(t, cfg.TLS)
				assert.Equal(t, tt.wantServerName, cfg.TLS.ServerName)
				assert.False(t, cfg.TLS.InsecureSkipVerify)
			}
			for key, want := range tt.wantParams {
				assert.Equal(t, want, cfg.Params[key], key)
			}
		})
	}
}

func mysqlMonitor(t *testing.T, timeout int, cfg MySQLConfig) *Monitor {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return &Monitor{ID: "mysql-test", Name: "MySQL test", Type: "mysql", Timeout: timeout, Config: string(raw)}
}

func TestMySQLExecutor_Execute_DownWithoutAServer(t *testing.T) {
	exec := NewMySQLExecutor(zap.NewNop().Sugar())
	refused := refusedAddr(t)

	tests := []struct {
		name        string
		config      MySQLConfig
		wantMessage string
	}{
		{
			name:        "an empty connection string",
			config:      MySQLConfig{Query: "SELECT 1"},
			wantMessage: "connection string validation failed: connection string cannot be empty",
		},
		{
			name:        "the connection string of another database",
			config:      MySQLConfig{ConnectionString: "postgres://user:password@localhost:5432/testdb", Query: "SELECT 1"},
			wantMessage: "connection string validation failed: connection string must use mysql:// scheme, got: postgres",
		},
		{
			// The query is rejected before the executor connects: nothing listens here.
			name:        "a query that changes data",
			config:      MySQLConfig{ConnectionString: "mysql://user:password@" + refused + "/testdb", Query: "DROP TABLE users"},
			wantMessage: "query validation failed: only SELECT, SHOW, DESCRIBE, and EXPLAIN statements are allowed for monitoring queries",
		},
		{
			name:        "an unknown tls setting",
			config:      MySQLConfig{ConnectionString: "mysql://user:password@" + refused + "/testdb?tls=sometimes"},
			wantMessage: "MySQL query failed: failed to parse MySQL connection string: invalid value / unknown config name: sometimes",
		},
		{
			name:        "a connection nothing accepts",
			config:      MySQLConfig{ConnectionString: "mysql://user:password@" + refused + "/testdb"},
			wantMessage: "MySQL query failed: failed to ping MySQL database: dial tcp " + refused + ": connect: connection refused",
		},
		{
			name:        "a connection nothing accepts, with a custom query",
			config:      MySQLConfig{ConnectionString: "mysql://user:password@" + refused + "/testdb", Query: "SELECT 2"},
			wantMessage: "MySQL query failed: failed to ping MySQL database: dial tcp " + refused + ": connect: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := exec.Execute(context.Background(), mysqlMonitor(t, 5, tt.config), nil)

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Equal(t, tt.wantMessage, result.Message)
			assert.False(t, result.StartTime.IsZero())
			assert.False(t, result.EndTime.Before(result.StartTime))
		})
	}
}

func TestMySQLExecutor_Execute_MalformedConfig(t *testing.T) {
	exec := NewMySQLExecutor(zap.NewNop().Sugar())

	result := exec.Execute(context.Background(), &Monitor{ID: "mysql-test", Timeout: 5, Config: `{"connection_string": "mysql://user:password@localhost:3306/testdb" "query": "SELECT 1"}`}, nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "invalid character")
}

func TestMySQLExecutor_Execute_ServerNeverAnswers(t *testing.T) {
	exec := NewMySQLExecutor(zap.NewNop().Sugar())
	addr := silentAddr(t)

	start := time.Now()
	result := exec.Execute(context.Background(), mysqlMonitor(t, 2, MySQLConfig{
		ConnectionString: "mysql://user:password@" + addr + "/testdb",
	}), nil)
	elapsed := time.Since(start)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "MySQL query failed: failed to ping MySQL database: ")
	assert.Less(t, elapsed, 3*time.Second, "the check must end within the monitor's timeout plus 1s")
}
