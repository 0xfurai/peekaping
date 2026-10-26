//go:build !slim

package executor

import (
	"context"
	"encoding/json"
	"peekaping/internal/modules/shared"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/denisenkom/go-mssqldb/msdsn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func sqlServerMonitor(t *testing.T, timeout int, cfg SQLServerConfig) *Monitor {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return &Monitor{ID: "sqlserver-test", Name: "SQL Server test", Type: "sqlserver", Timeout: timeout, Config: string(raw)}
}

func TestSQLServerExecutor_ServerRefusesConnection(t *testing.T) {
	addr := refusedAddr(t)

	exec := NewSQLServerExecutor(zap.NewNop().Sugar())
	start := time.Now()
	result := exec.Execute(context.Background(), sqlServerMonitor(t, 2, SQLServerConfig{
		DatabaseConnectionString: "Server=" + strings.Replace(addr, ":", ",", 1) + ";Database=master;User Id=sa;Password=secret;Encrypt=false",
	}), nil)
	elapsed := time.Since(start)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "connection failed: unable to open tcp connection with host '"+addr+"': dial tcp "+addr+": connect: connection refused")
	assert.Less(t, elapsed, 3*time.Second, "the check must end within the monitor's timeout plus 1s")
}

func TestSQLServerExecutor_ServerNeverAnswers(t *testing.T) {
	// A Connection Timeout in the connection string makes the driver move its
	// own deadline with every read, which must not outlast the monitor's.
	for name, extra := range map[string]string{"default": "", "with a longer Connection Timeout": ";Connection Timeout=30"} {
		t.Run(name, func(t *testing.T) {
			addr, accepted := silentServer(t)

			exec := NewSQLServerExecutor(zap.NewNop().Sugar())
			start := time.Now()
			result := exec.Execute(context.Background(), sqlServerMonitor(t, 2, SQLServerConfig{
				DatabaseConnectionString: "Server=" + strings.Replace(addr, ":", ",", 1) + ";Database=master;User Id=sa;Password=secret;Encrypt=false" + extra,
			}), nil)
			elapsed := time.Since(start)

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Regexp(t, `^connection failed: read tcp 127\.0\.0\.1:\d+->`+regexp.QuoteMeta(addr)+`: i/o timeout$`, result.Message)
			assert.Equal(t, 1, accepted(), "the executor should have connected once")
			assert.Less(t, elapsed, 3*time.Second, "the check must end within the monitor's timeout plus 1s")
		})
	}
}

func TestSQLServerExecutor_BracedUserAndDatabaseWithSemicolonsReachTheConnection(t *testing.T) {
	addr := refusedAddr(t)

	exec := NewSQLServerExecutor(zap.NewNop().Sugar())
	result := exec.Execute(context.Background(), sqlServerMonitor(t, 2, SQLServerConfig{
		DatabaseConnectionString: "Server=" + strings.Replace(addr, ":", ",", 1) + ";Database={d;e};User Id={u;v};Password={p;q};Encrypt=false",
	}), nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "connection failed: unable to open tcp connection with host '"+addr+"'", "the string was refused before connecting")
}

func TestSQLServerExecutor_RefusesAQueryBeforeConnecting(t *testing.T) {
	// Nothing listens at the address, so a connection attempt would be the
	// message of the result.
	addr := refusedAddr(t)

	exec := NewSQLServerExecutor(zap.NewNop().Sugar())
	start := time.Now()
	result := exec.Execute(context.Background(), sqlServerMonitor(t, 2, SQLServerConfig{
		DatabaseConnectionString: "Server=" + strings.Replace(addr, ":", ",", 1) + ";Database=master;User Id=sa;Password=secret;Encrypt=false",
		DatabaseQuery:            "DELETE FROM accounts",
	}), nil)
	elapsed := time.Since(start)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Equal(t, "query validation failed: query must start with one of: SELECT, SHOW, DESCRIBE, DESC, EXPLAIN, WITH, VALUES", result.Message)
	assert.Less(t, elapsed, time.Second)
}

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
				// A string with nothing to escape goes to the driver as it is.
				assert.Equal(t, tt.expectedDSN, dsn)
			}
		})
	}
}

// What the driver makes of a DSN: the test checks the values that reach the
// server, not the spelling of the string that carries them.
type driverView struct {
	Host, Instance, User, Password, Database string
	Port                                     uint64
	Encryption                               msdsn.Encryption
	AppName                                  string
	ConnTimeout                              time.Duration
}

func driverViewOf(t *testing.T, dsn string) driverView {
	t.Helper()
	cfg, _, err := msdsn.Parse(dsn)
	require.NoError(t, err, dsn)
	if cfg.AppName == "go-mssqldb" { // the driver's default
		cfg.AppName = ""
	}
	return driverView{
		Host: cfg.Host, Instance: cfg.Instance, User: cfg.User, Password: cfg.Password, Database: cfg.Database,
		Port: cfg.Port, Encryption: cfg.Encryption, AppName: cfg.AppName, ConnTimeout: cfg.ConnTimeout,
	}
}

func TestSQLServerExecutor_parseConnectionString_driverSeesTheValues(t *testing.T) {
	executor := NewSQLServerExecutor(zap.NewNop().Sugar())

	tests := []struct {
		name          string
		connectionStr string
		want          driverView
	}{
		{
			name:          "URL: sqlserver scheme",
			connectionStr: "sqlserver://sa:password@localhost:1433?database=master",
			want:          driverView{Host: "localhost", Port: 1433, User: "sa", Password: "password", Database: "master"},
		},
		{
			name:          "URL: mssql scheme",
			connectionStr: "mssql://user:pass@server:1433?database=testdb",
			want:          driverView{Host: "server", Port: 1433, User: "user", Password: "pass", Database: "testdb"},
		},
		{
			name:          "URL: default port",
			connectionStr: "sqlserver://sa:password@localhost?database=master",
			want:          driverView{Host: "localhost", User: "sa", Password: "password", Database: "master"},
		},
		{
			name:          "URL: database in the path",
			connectionStr: "sqlserver://sa:password@localhost:1433/mydb",
			want:          driverView{Host: "localhost", Port: 1433, User: "sa", Password: "password", Database: "mydb"},
		},
		{
			name:          "URL: the query's database wins over the path's",
			connectionStr: "sqlserver://sa:password@localhost:1433/pathdb?database=querydb",
			want:          driverView{Host: "localhost", Port: 1433, User: "sa", Password: "password", Database: "querydb"},
		},
		{
			name:          "URL: other parameters reach the driver",
			connectionStr: "sqlserver://user:pass@server:1433?database=testdb&encrypt=disable&trustServerCertificate=true&app%20name=peekaping",
			want:          driverView{Host: "server", Port: 1433, User: "user", Password: "pass", Database: "testdb", Encryption: msdsn.EncryptionDisabled, AppName: "peekaping"},
		},
		{
			name:          "URL: timeout is the Connection Timeout",
			connectionStr: "sqlserver://sa:password@localhost:1433?database=master&timeout=7",
			want:          driverView{Host: "localhost", Port: 1433, User: "sa", Password: "password", Database: "master", ConnTimeout: 7 * time.Second},
		},
		{
			name:          "URL: an IPv6 host with a port",
			connectionStr: "sqlserver://u:p@[::1]:1433?database=m",
			want:          driverView{Host: "::1", Port: 1433, User: "u", Password: "p", Database: "m"},
		},
		{
			name:          "URL: an IPv6 host without a port",
			connectionStr: "sqlserver://u:p@[::1]?database=m",
			want:          driverView{Host: "::1", Port: 1433, User: "u", Password: "p", Database: "m"},
		},
		{
			name:          "URL: a percent-encoded password with ; = { and }",
			connectionStr: "sqlserver://sa:P%3Bss%3D%7Bw%7D@localhost:1433?database=master",
			want:          driverView{Host: "localhost", Port: 1433, User: "sa", Password: "P;ss={w}", Database: "master"},
		},
		{
			name:          "URL: a password can't add parameters",
			connectionStr: "sqlserver://sa:x%3BEncrypt%3Ddisable%3Bdatabase%3Dother@localhost:1433?database=master",
			want:          driverView{Host: "localhost", Port: 1433, User: "sa", Password: "x;Encrypt=disable;database=other", Database: "master"},
		},
		{
			name:          "URL: a user and a database with ; and =",
			connectionStr: "sqlserver://a%3Bb%3Dc:pw@localhost:1433?database=d%3Be%3Df",
			want:          driverView{Host: "localhost", Port: 1433, User: "a;b=c", Password: "pw", Database: "d;e=f"},
		},
		{
			name:          "Server=: a braced password with ; = { and }",
			connectionStr: "Server=localhost,1433;Database=master;User Id=sa;Password={P;ss={w}}};Encrypt=false;TrustServerCertificate=true",
			want:          driverView{Host: "localhost", Port: 1433, User: "sa", Password: "P;ss={w}", Database: "master"},
		},
		{
			name:          "Server=: a braced password can't add parameters",
			connectionStr: "Server=localhost,1433;Database=master;User Id=sa;Password={x;Encrypt=disable;Database=other};AppName=ignored;Application Name=peekaping",
			want:          driverView{Host: "localhost", Port: 1433, User: "sa", Password: "x;Encrypt=disable;Database=other", Database: "master", AppName: "peekaping"},
		},
		{
			name:          "Server=: a braced user and database with ; and }, a tcp: prefix and an instance",
			connectionStr: "server=tcp:myhost\\inst,1433;DATABASE={d;e};user id={u;v=w}}x};password={p}",
			want:          driverView{Host: "myhost", Instance: "inst", Port: 1433, User: "u;v=w}x", Password: "p", Database: "d;e"},
		},
		{
			name:          "Server=: a braced password with spaces around the braces",
			connectionStr: "Server=localhost;Database=master;User Id=sa;Password= {a b} ;Encrypt=false",
			want:          driverView{Host: "localhost", User: "sa", Password: "a b", Database: "master"},
		},
		{
			name:          "Server=: nothing braced is passed through as it is",
			connectionStr: "Server=localhost,1433;Database=master;User Id=sa;Password=plain;Encrypt=false;TrustServerCertificate=true",
			want:          driverView{Host: "localhost", Port: 1433, User: "sa", Password: "plain", Database: "master"},
		},
		{
			name:          "Server=: a password that starts with a brace but is not braced stays as it is",
			connectionStr: "Server=localhost;Database=master;User Id=sa;Password={secret",
			want:          driverView{Host: "localhost", User: "sa", Password: "{secret", Database: "master"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// What Validate accepts is what Execute connects with.
			raw, err := json.Marshal(SQLServerConfig{DatabaseConnectionString: tt.connectionStr})
			require.NoError(t, err)
			require.NoError(t, executor.Validate(string(raw)))

			dsn, err := executor.parseConnectionString(tt.connectionStr)
			require.NoError(t, err)

			assert.Equal(t, tt.want, driverViewOf(t, dsn))
		})
	}
}

func TestSQLServerExecutor_Validate_bracedValues(t *testing.T) {
	executor := NewSQLServerExecutor(zap.NewNop().Sugar())

	// The braced password holds what would look like other parameters: a bad
	// Encrypt value and a second Database.
	err := executor.Validate(`{"database_connection_string": "Server=localhost,1433;Database=master;User Id=sa;Password={x;Encrypt=maybe;Database=;Server=};Encrypt=false"}`)

	assert.NoError(t, err)
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
