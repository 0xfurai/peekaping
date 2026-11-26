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
func TestPostgresExecutor_Integration(t *testing.T) {
	srv := testservice.Postgres(t)
	exec := NewPostgresExecutor(zap.NewNop().Sugar())

	run := func(t *testing.T, connectionString, query string) *Result {
		t.Helper()
		cfg, err := json.Marshal(PostgresConfig{DatabaseConnectionString: connectionString, DatabaseQuery: query})
		require.NoError(t, err)
		return exec.Execute(context.Background(), &Monitor{
			ID:      "integration-postgres",
			Name:    "Integration PostgreSQL",
			Type:    "postgres",
			Timeout: 5,
			Config:  string(cfg),
		}, nil)
	}

	t.Run("is up with the default query", func(t *testing.T) {
		result := run(t, srv.URL(srv.Password, ""), "")

		assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		assert.Contains(t, result.Message, "Query successful")
	})

	t.Run("is up with a custom SELECT", func(t *testing.T) {
		result := run(t, srv.URL(srv.Password, ""), "SELECT current_user, current_database()")

		assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
	})

	t.Run("is down with a wrong password", func(t *testing.T) {
		result := run(t, srv.URL("not-"+srv.Password, ""), "")

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, `connection failed: pgdriver: SASL: FATAL: password authentication failed for user "peekaping" (SQLSTATE=28P01)`)
	})

	t.Run("is down with a missing database", func(t *testing.T) {
		missing := srv
		missing.Database = "no_such_database"

		result := run(t, missing.URL(srv.Password, ""), "")

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, `connection failed: FATAL: database "no_such_database" does not exist (SQLSTATE=3D000)`)
	})

	t.Run("is down when the query fails", func(t *testing.T) {
		result := run(t, srv.URL(srv.Password, ""), "SELECT 1/0")

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "Query failed: ERROR: division by zero (SQLSTATE=22012)")
	})

	t.Run("ignores query parameters other than sslmode", func(t *testing.T) {
		// pgdriver would send each of these to the server as SET name TO value,
		// which runs statements the query validator never saw.
		for _, query := range []string{"foo=bar", "options=-c%20statement_timeout%3D1", "statement_timeout=1", "application_name=x"} {
			result := run(t, srv.URL(srv.Password, query), "SELECT pg_sleep(0.3)")

			assert.Equal(t, shared.MonitorStatusUp, result.Status, query+": "+result.Message)
		}
	})

	// A query that fails unless the connection's TLS state is the wanted one,
	// since a plain SELECT 1 is Up either way. The divisor comes from a
	// volatile function: PostgreSQL folds a constant 1/0 while planning, and
	// fails even in a CASE branch it never takes.
	const onlyOverTLS = "SELECT CASE WHEN ssl THEN 1 ELSE 1/(random()*0)::int END FROM pg_stat_ssl WHERE pid = pg_backend_pid()"
	const onlyWithoutTLS = "SELECT CASE WHEN ssl THEN 1/(random()*0)::int ELSE 1 END FROM pg_stat_ssl WHERE pid = pg_backend_pid()"

	t.Run("sslmode", func(t *testing.T) {
		t.Run("require is up over TLS", func(t *testing.T) {
			result := run(t, srv.URL(srv.Password, "sslmode=require"), onlyOverTLS)

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		})

		t.Run("disable is up without TLS", func(t *testing.T) {
			result := run(t, srv.URL(srv.Password, "sslmode=disable"), onlyWithoutTLS)

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		})

		t.Run("no sslmode connects without TLS", func(t *testing.T) {
			result := run(t, srv.URL(srv.Password, ""), onlyWithoutTLS)

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		})

		t.Run("the TLS check does fail on the wrong state", func(t *testing.T) {
			result := run(t, srv.URL(srv.Password, "sslmode=require"), onlyWithoutTLS)

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Contains(t, result.Message, "division by zero")
		})

		for _, mode := range []string{"verify-ca", "verify-full"} {
			t.Run(mode+" is down with a certificate error", func(t *testing.T) {
				result := run(t, srv.URL(srv.Password, "sslmode="+mode), "")

				assert.Equal(t, shared.MonitorStatusDown, result.Status, result.Message)
				// The words after x509: depend on the system's certificate
				// verifier: "certificate signed by unknown authority" on
				// Linux, "certificate is not standards compliant" on macOS.
				assert.Contains(t, result.Message, "connection failed: pgdriver: TLS handshake failed:")
				assert.Contains(t, result.Message, "x509: ")
			})
		}

		t.Run("an unknown sslmode is down with the driver's message", func(t *testing.T) {
			result := run(t, srv.URL(srv.Password, "sslmode=sometimes"), "")

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Contains(t, result.Message, "pgdriver: sslmode 'sometimes' is not supported")
		})

		t.Run("sslrootcert is ignored", func(t *testing.T) {
			// It names a file on the Peekaping server, which the check
			// doesn't read: require is Up although the file is missing.
			result := run(t, srv.URL(srv.Password, "sslmode=require&sslrootcert=/no/such/root.crt"), onlyOverTLS)

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		})
	})
}
