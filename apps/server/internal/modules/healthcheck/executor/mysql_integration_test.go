//go:build integration

package executor

import (
	"context"
	"encoding/json"
	"testing"

	"peekaping/internal/modules/shared"
	"peekaping/internal/testservice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The server has TLS on with a self-signed certificate, and its user's
// password has reserved characters (@ : / % ?), so every Up below also shows
// that the password reaches the server intact.
func TestMySQLExecutor_Integration(t *testing.T) {
	srv := testservice.MySQL(t)
	exec := NewMySQLExecutor(zap.NewNop().Sugar())

	run := func(t *testing.T, connectionString, query string) *Result {
		t.Helper()
		cfg, err := json.Marshal(MySQLConfig{ConnectionString: connectionString, Query: query})
		require.NoError(t, err)
		return exec.Execute(context.Background(), &Monitor{
			ID:      "integration-mysql",
			Name:    "Integration MySQL",
			Type:    "mysql",
			Timeout: 5,
			Config:  string(cfg),
		}, nil)
	}

	t.Run("is up with the default query", func(t *testing.T) {
		for _, query := range []string{"", "   \t\n   "} {
			result := run(t, srv.URL(srv.Password, ""), query)

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
			assert.Contains(t, result.Message, "Query successful")
			assert.Contains(t, result.Message, "Rows: 1", "SELECT 1 is the default query")
		}
	})

	t.Run("is up with a custom SELECT", func(t *testing.T) {
		result := run(t, srv.URL(srv.Password, ""), "SELECT 1 UNION SELECT 2 UNION SELECT 3")

		assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		assert.Contains(t, result.Message, "Rows: 3")
	})

	t.Run("is up with query parameters", func(t *testing.T) {
		result := run(t, srv.URL(srv.Password, "charset=utf8mb4&timeout=3s"), "SELECT DATABASE(), CURRENT_USER()")

		assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
	})

	t.Run("is down with a wrong password", func(t *testing.T) {
		result := run(t, srv.URL("not-"+srv.Password, ""), "")

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "MySQL query failed: failed to ping MySQL database: Error 1045 (28000): Access denied for user 'peekaping'@")
	})

	t.Run("is down with a missing database", func(t *testing.T) {
		missing := srv
		missing.Database = "no_such_database"

		result := run(t, missing.URL(srv.Password, ""), "")

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "MySQL query failed: failed to ping MySQL database: Error 1044 (42000): Access denied for user 'peekaping'@'%' to database 'no_such_database'")
	})

	t.Run("is down when the query fails", func(t *testing.T) {
		result := run(t, srv.URL(srv.Password, ""), "SELECT * FROM no_such_table")

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "MySQL query failed: failed to execute query: Error 1146 (42S02): Table 'peekaping.no_such_table' doesn't exist")
	})

	// A query that fails unless the connection's TLS state is the wanted one,
	// since a plain SELECT 1 is Up either way. IF evaluates only the branch it
	// takes, and the second subquery fails because it returns two rows.
	const sslCipher = "(SELECT VARIABLE_VALUE FROM performance_schema.session_status WHERE VARIABLE_NAME = 'Ssl_cipher')"
	const onlyOverTLS = "SELECT IF(" + sslCipher + " <> '', 1, (SELECT 1 UNION SELECT 2))"
	const onlyWithoutTLS = "SELECT IF(" + sslCipher + " = '', 1, (SELECT 1 UNION SELECT 2))"

	t.Run("tls", func(t *testing.T) {
		t.Run("skip-verify is up over TLS", func(t *testing.T) {
			result := run(t, srv.URL(srv.Password, "tls=skip-verify"), onlyOverTLS)

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		})

		t.Run("false is up without TLS", func(t *testing.T) {
			result := run(t, srv.URL(srv.Password, "tls=false"), onlyWithoutTLS)

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		})

		t.Run("the TLS check does fail on the wrong state", func(t *testing.T) {
			result := run(t, srv.URL(srv.Password, "tls=false"), onlyOverTLS)

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Contains(t, result.Message, "Error 1242 (21000): Subquery returns more than 1 row")
		})

		t.Run("true is down with a certificate error", func(t *testing.T) {
			result := run(t, srv.URL(srv.Password, "tls=true"), "")

			assert.Equal(t, shared.MonitorStatusDown, result.Status, result.Message)
			// The words after x509: depend on the system's certificate
			// verifier: "certificate signed by unknown authority" on
			// Linux, "certificate is not standards compliant" on macOS.
			assert.Contains(t, result.Message, "MySQL query failed: failed to ping MySQL database: ")
			assert.Contains(t, result.Message, "x509: ")
		})

		t.Run("an unknown tls setting is down with the driver's message", func(t *testing.T) {
			result := run(t, srv.URL(srv.Password, "tls=sometimes"), "")

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Contains(t, result.Message, "MySQL query failed: failed to parse MySQL connection string: invalid value / unknown config name: sometimes")
		})
	})
}
