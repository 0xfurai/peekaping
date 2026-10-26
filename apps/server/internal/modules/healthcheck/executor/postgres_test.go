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

func TestNewPostgresConnector(t *testing.T) {
	tests := []struct {
		name             string
		connectionString string
		wantAddr         string
		wantTLS          bool // a TLS config is set
		wantVerify       bool // and it checks the certificate chain
		wantChainCheck   bool // and it checks the chain itself, not the host name (verify-ca)
		wantServerName   string
		wantErr          string
	}{
		{
			name:             "host and port",
			connectionString: "postgres://user:password@localhost:5433/testdb",
			wantAddr:         "localhost:5433",
		},
		{
			name:             "default port",
			connectionString: "postgres://user:password@localhost/testdb",
			wantAddr:         "localhost:5432",
		},
		{
			name:             "postgresql scheme",
			connectionString: "postgresql://user:password@localhost:5432/testdb",
			wantAddr:         "localhost:5432",
		},
		{
			name:             "IPv6 host with a port",
			connectionString: "postgres://user:password@[::1]:5432/testdb",
			wantAddr:         "[::1]:5432",
		},
		{
			name:             "IPv6 host with another port",
			connectionString: "postgres://user:password@[2001:db8::7]:6543/testdb",
			wantAddr:         "[2001:db8::7]:6543",
		},
		{
			name:             "IPv6 host without a port",
			connectionString: "postgres://user:password@[::1]/testdb",
			wantAddr:         "[::1]:5432",
		},
		{
			name:             "an empty sslmode means no TLS",
			connectionString: "postgres://user:password@localhost:5432/testdb?sslmode=",
			wantAddr:         "localhost:5432",
		},
		{
			name:             "sslmode=disable means no TLS",
			connectionString: "postgres://user:password@localhost:5432/testdb?sslmode=disable",
			wantAddr:         "localhost:5432",
		},
		{
			name:             "sslmode=require uses TLS without checking the certificate",
			connectionString: "postgres://user:password@localhost:5432/testdb?sslmode=require",
			wantAddr:         "localhost:5432",
			wantTLS:          true,
		},
		{
			name:             "sslmode=verify-full checks the certificate",
			connectionString: "postgres://user:password@localhost:5432/testdb?sslmode=verify-full",
			wantAddr:         "localhost:5432",
			wantTLS:          true,
			wantVerify:       true,
		},
		{
			name:             "sslmode=allow uses TLS without checking the certificate",
			connectionString: "postgres://user:password@localhost:5432/testdb?sslmode=allow",
			wantAddr:         "localhost:5432",
			wantTLS:          true,
		},
		{
			name:             "sslmode=prefer uses TLS without checking the certificate",
			connectionString: "postgres://user:password@localhost:5432/testdb?sslmode=prefer",
			wantAddr:         "localhost:5432",
			wantTLS:          true,
		},
		{
			name:             "sslmode=verify-ca checks the certificate chain",
			connectionString: "postgres://user:password@localhost:5432/testdb?sslmode=verify-ca",
			wantAddr:         "localhost:5432",
			wantTLS:          true,
			wantChainCheck:   true,
		},
		{
			name:             "sslmode is not case sensitive: REQUIRE",
			connectionString: "postgres://user:password@localhost:5432/testdb?sslmode=REQUIRE",
			wantAddr:         "localhost:5432",
			wantTLS:          true,
		},
		{
			name:             "sslmode is not case sensitive: Require",
			connectionString: "postgres://user:password@localhost:5432/testdb?sslmode=Require",
			wantAddr:         "localhost:5432",
			wantTLS:          true,
		},
		{
			name:             "sslmode is not case sensitive: Verify-Full",
			connectionString: "postgres://user:password@localhost:5432/testdb?sslmode=Verify-Full",
			wantAddr:         "localhost:5432",
			wantTLS:          true,
			wantVerify:       true,
			wantServerName:   "localhost",
		},
		{
			name:             "sslmode is not case sensitive: DISABLE",
			connectionString: "postgres://user:password@localhost:5432/testdb?sslmode=DISABLE",
			wantAddr:         "localhost:5432",
		},
		{
			name:             "verify-full checks the certificate against the host name",
			connectionString: "postgres://user:password@db.example.com:5433/testdb?sslmode=verify-full",
			wantAddr:         "db.example.com:5433",
			wantTLS:          true,
			wantVerify:       true,
			wantServerName:   "db.example.com",
		},
		{
			name:             "verify-full with an IPv6 host checks against the address without brackets",
			connectionString: "postgres://user:password@[::1]:5432/testdb?sslmode=verify-full",
			wantAddr:         "[::1]:5432",
			wantTLS:          true,
			wantVerify:       true,
			wantServerName:   "::1",
		},
		{
			name:             "verify-full with an IPv6 host and no port checks against the address without brackets",
			connectionString: "postgres://user:password@[::1]/testdb?sslmode=verify-full",
			wantAddr:         "[::1]:5432",
			wantTLS:          true,
			wantVerify:       true,
			wantServerName:   "::1",
		},
		{
			name:             "sslrootcert is ignored: it names a file on the Peekaping server",
			connectionString: "postgres://user:password@localhost:5432/testdb?sslmode=require&sslrootcert=/no/such/root.crt",
			wantAddr:         "localhost:5432",
			wantTLS:          true,
		},
		{
			name:             "other query parameters are ignored",
			connectionString: "postgres://user:password@localhost:5432/testdb?foo=bar&options=-c%20statement_timeout%3D1&application_name=x&connect_timeout=1",
			wantAddr:         "localhost:5432",
		},
		{
			name:             "a host parameter can't redirect the connection to a socket",
			connectionString: "postgres://user:password@localhost:5432/testdb?host=/var/run/postgresql",
			wantAddr:         "localhost:5432",
		},
		{
			name:             "unknown sslmode",
			connectionString: "postgres://user:password@localhost:5432/testdb?sslmode=sometimes",
			wantErr:          "pgdriver: sslmode 'sometimes' is not supported",
		},
		{
			name:             "no password",
			connectionString: "postgres://user@localhost:5432/testdb",
			wantErr:          "password is undefined",
		},
		{
			name:             "empty password",
			connectionString: "postgres://user:@localhost:5432/testdb",
			wantErr:          "password is undefined",
		},
		{
			name:             "malformed connection string",
			connectionString: "not-a-valid-url",
			wantErr:          "invalid scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connector, err := newPostgresConnector(tt.connectionString)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)

			cfg := connector.Config()
			assert.Equal(t, "tcp", cfg.Network)
			assert.Equal(t, tt.wantAddr, cfg.Addr)
			assert.Equal(t, "user", cfg.User)
			assert.Equal(t, "password", cfg.Password)
			assert.Equal(t, "testdb", cfg.Database)
			assert.Empty(t, cfg.ConnParams, "no parameter is sent to the server as SET")
			if !tt.wantTLS {
				assert.Nil(t, cfg.TLSConfig)
				return
			}
			require.NotNil(t, cfg.TLSConfig)
			assert.Equal(t, !tt.wantVerify, cfg.TLSConfig.InsecureSkipVerify)
			assert.Equal(t, tt.wantChainCheck, cfg.TLSConfig.VerifyPeerCertificate != nil)
			assert.Nil(t, cfg.TLSConfig.RootCAs, "no root certificate is read from the server's disk")
			if tt.wantServerName != "" {
				assert.Equal(t, tt.wantServerName, cfg.TLSConfig.ServerName)
			}
		})
	}
}

func TestNewPostgresConnector_NoSSLModeMeansNoTLS(t *testing.T) {
	connector, err := newPostgresConnector("postgres://user:password@localhost:5432/testdb")
	require.NoError(t, err)

	assert.Nil(t, connector.Config().TLSConfig)
}

func TestNewPostgresConnector_PercentEncodedCredentials(t *testing.T) {
	connector, err := newPostgresConnector("postgres://us%40er:p%40ss%3Aw%2Frd%251%3Fx@localhost:5432/test%20db")
	require.NoError(t, err)

	cfg := connector.Config()
	assert.Equal(t, "us@er", cfg.User)
	assert.Equal(t, "p@ss:w/rd%1?x", cfg.Password)
	assert.Equal(t, "test db", cfg.Database)
}

func postgresMonitor(t *testing.T, timeout int, cfg PostgresConfig) *Monitor {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return &Monitor{ID: "postgres-test", Name: "PostgreSQL test", Type: "postgres", Timeout: timeout, Config: string(raw)}
}

func TestPostgresExecutor_Execute_DownWithoutAServer(t *testing.T) {
	exec := NewPostgresExecutor(zap.NewNop().Sugar())
	refused := refusedAddr(t)

	tests := []struct {
		name        string
		config      PostgresConfig
		wantMessage string
	}{
		{
			name:        "an empty connection string",
			config:      PostgresConfig{},
			wantMessage: "connection string validation failed: connection string cannot be empty",
		},
		{
			name:        "the connection string of another database",
			config:      PostgresConfig{DatabaseConnectionString: "mysql://user:password@localhost:3306/testdb"},
			wantMessage: "connection string validation failed: connection string must use one of [postgres postgresql] schemes, got: mysql",
		},
		{
			name:        "no password",
			config:      PostgresConfig{DatabaseConnectionString: "postgres://user@" + refused + "/testdb"},
			wantMessage: "failed to parse connection string: password is undefined",
		},
		{
			name:        "an unknown sslmode",
			config:      PostgresConfig{DatabaseConnectionString: "postgres://user:password@" + refused + "/testdb?sslmode=sometimes"},
			wantMessage: "failed to parse connection string: pgdriver: sslmode 'sometimes' is not supported",
		},
		{
			// The query is rejected before the executor connects: nothing listens here.
			name:        "a query that changes data",
			config:      PostgresConfig{DatabaseConnectionString: "postgres://user:password@" + refused + "/testdb", DatabaseQuery: "DROP TABLE users"},
			wantMessage: "query validation failed: only SELECT, SHOW, DESCRIBE, EXPLAIN, WITH, and VALUES statements are allowed for monitoring queries",
		},
		{
			name:        "a connection nothing accepts",
			config:      PostgresConfig{DatabaseConnectionString: "postgres://user:password@" + refused + "/testdb"},
			wantMessage: "connection failed: dial tcp " + refused + ": connect: connection refused",
		},
		{
			name:        "a connection nothing accepts, with a custom query",
			config:      PostgresConfig{DatabaseConnectionString: "postgres://user:password@" + refused + "/testdb", DatabaseQuery: "SELECT 2"},
			wantMessage: "connection failed: dial tcp " + refused + ": connect: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := exec.Execute(context.Background(), postgresMonitor(t, 5, tt.config), nil)

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Equal(t, tt.wantMessage, result.Message)
			assert.False(t, result.StartTime.IsZero())
			assert.False(t, result.EndTime.Before(result.StartTime))
		})
	}
}

func TestPostgresExecutor_Execute_MalformedConfig(t *testing.T) {
	exec := NewPostgresExecutor(zap.NewNop().Sugar())

	result := exec.Execute(context.Background(), &Monitor{ID: "postgres-test", Timeout: 5, Config: `{"database_connection_string": "postgres://user:password@localhost:5432/testdb" "database_query": "SELECT 1"}`}, nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "invalid character")
}

func TestPostgresExecutor_Execute_ServerNeverAnswers(t *testing.T) {
	exec := NewPostgresExecutor(zap.NewNop().Sugar())
	addr := silentAddr(t)

	start := time.Now()
	result := exec.Execute(context.Background(), postgresMonitor(t, 2, PostgresConfig{
		DatabaseConnectionString: "postgres://user:password@" + addr + "/testdb",
	}), nil)
	elapsed := time.Since(start)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "connection failed: read tcp ")
	assert.Contains(t, result.Message, ": i/o timeout")
	assert.Less(t, elapsed, 3*time.Second, "the check must end within the monitor's timeout plus 1s")
}

func TestPostgresExecutor_validateConnectionString(t *testing.T) {
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
			err := ValidateConnectionString(tt.connectionStr, []string{"postgres", "postgresql"})
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
