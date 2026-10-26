//go:build integration && !slim

package executor

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"testing"
	"time"

	"peekaping/internal/modules/shared"
	"peekaping/internal/testservice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	// A login with a password that needs no escaping, for the cases that are
	// about something other than the escaping. sa's password has ; = { and }.
	sqlServerPlainUser     = "peekaping_plain"
	sqlServerPlainPassword = "Plain-Passw0rd"

	// What the Server= form needs to reach a server with a self-signed
	// certificate, which is what the compose server has.
	unverifiedTLS = "Encrypt=false;TrustServerCertificate=true"
)

// createPlainLogin makes the login on the server, if it isn't there from an
// earlier run (the compose server outlives a run).
func createPlainLogin(t *testing.T, srv testservice.SQLServerServer) {
	t.Helper()
	dsn := (&url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(srv.Username, srv.Password),
		Host:     srv.Addr(),
		RawQuery: url.Values{"database": {"master"}, "encrypt": {"disable"}}.Encode(),
	}).String()
	db, err := sql.Open("sqlserver", dsn)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`IF NOT EXISTS (SELECT 1 FROM sys.sql_logins WHERE name = '` + sqlServerPlainUser + `')
		CREATE LOGIN ` + sqlServerPlainUser + ` WITH PASSWORD = '` + sqlServerPlainPassword + `', CHECK_POLICY = OFF`)
	require.NoError(t, err)
}

// adoString is the connection string in the Server=...;Database=... form. The
// password is written as given: brace it first when it needs it.
func adoString(srv testservice.SQLServerServer, database, user, password, extra string) string {
	s := "Server=" + srv.Host + "," + srv.Port + ";Database=" + database + ";User Id=" + user + ";Password=" + password
	if extra != "" {
		s += ";" + extra
	}
	return s
}

// urlString is the connection string in the sqlserver:// form, with the
// credentials percent-encoded.
func urlString(srv testservice.SQLServerServer, database, user, password string, query url.Values) string {
	q := url.Values{"database": {database}}
	for k, v := range query {
		q[k] = v
	}
	return (&url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(user, password),
		Host:     srv.Addr(),
		RawQuery: q.Encode(),
	}).String()
}

// bracedValue writes a value for the Server=... form so that it may hold ; =
// { and }: in braces, with } doubled.
func bracedValue(v string) string {
	return "{" + strings.ReplaceAll(v, "}", "}}") + "}"
}

func TestSQLServerExecutor_Integration(t *testing.T) {
	srv := testservice.SQLServer(t)
	createPlainLogin(t, srv)
	exec := NewSQLServerExecutor(zap.NewNop().Sugar())

	run := func(t *testing.T, connectionString, query string) *Result {
		t.Helper()
		return exec.Execute(context.Background(), sqlServerMonitor(t, 10, SQLServerConfig{
			DatabaseConnectionString: connectionString,
			DatabaseQuery:            query,
		}), nil)
	}
	plainADO := func(extra string) string {
		return adoString(srv, "master", sqlServerPlainUser, sqlServerPlainPassword, extra)
	}
	plainURL := func(query url.Values) string {
		return urlString(srv, "master", sqlServerPlainUser, sqlServerPlainPassword, query)
	}
	noTLS := url.Values{"encrypt": {"disable"}}

	forms := []struct {
		name             string
		connectionString func() string
	}{
		{"Server= form", func() string { return plainADO(unverifiedTLS) }},
		{"sqlserver:// form", func() string { return plainURL(noTLS) }},
	}
	for _, form := range forms {
		t.Run(form.name, func(t *testing.T) {
			t.Run("is up for the default query", func(t *testing.T) {
				result := run(t, form.connectionString(), "")

				assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
				assert.Regexp(t, `^Query successful, ping: \d+ms, columns: 1, rows: 1$`, result.Message)
			})

			t.Run("is up for a custom SELECT", func(t *testing.T) {
				result := run(t, form.connectionString(), "SELECT name, database_id FROM sys.databases WHERE name IN ('master', 'tempdb') ORDER BY name")

				assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
				assert.Regexp(t, `^Query successful, ping: \d+ms, columns: 2, rows: 2$`, result.Message)
			})

			t.Run("is down when the query fails", func(t *testing.T) {
				result := run(t, form.connectionString(), "SELECT * FROM no_such_table")

				assert.Equal(t, shared.MonitorStatusDown, result.Status)
				assert.Equal(t, "query execution failed: mssql: Invalid object name 'no_such_table'.", result.Message)
			})
		})
	}

	// The Dial Timeout limits dialing; a query on the connection gets the
	// monitor's whole timeout.
	t.Run("is up for a query that takes longer than the Dial Timeout", func(t *testing.T) {
		start := time.Now()
		result := run(t, plainADO(unverifiedTLS+";Dial Timeout=2"), "SELECT 1 AS a; WAITFOR DELAY '00:00:04'; SELECT 2 AS b")

		assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		assert.GreaterOrEqual(t, time.Since(start), 4*time.Second, "the query should have waited")
	})

	t.Run("is down for a wrong password", func(t *testing.T) {
		result := run(t, adoString(srv, "master", sqlServerPlainUser, "not-"+sqlServerPlainPassword, unverifiedTLS), "")

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Equal(t, "connection failed: mssql: login error: Login failed for user '"+sqlServerPlainUser+"'.", result.Message)
	})

	t.Run("is down for a database that does not exist", func(t *testing.T) {
		result := run(t, adoString(srv, "no_such_database", sqlServerPlainUser, sqlServerPlainPassword, unverifiedTLS), "")

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Equal(t, `connection failed: mssql: login error: Cannot open database "no_such_database" that was requested by the login. Using the user default database "master" instead.`, result.Message)
	})

	t.Run("certificate", func(t *testing.T) {
		t.Run("is down when it must be verified and is self-signed", func(t *testing.T) {
			result := run(t, plainADO("Encrypt=true;TrustServerCertificate=false"), "")

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Contains(t, result.Message, "connection failed: TLS Handshake failed: ")
			assert.Contains(t, result.Message, "x509: ")
		})

		t.Run("is up when it is trusted", func(t *testing.T) {
			result := run(t, plainADO("Encrypt=true;TrustServerCertificate=true"), "")

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		})

		t.Run("is up when encryption is off and the certificate is trusted", func(t *testing.T) {
			result := run(t, plainADO("Encrypt=false;TrustServerCertificate=true"), "")

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		})

		// The driver still encrypts the login when Encrypt is false, and it
		// verifies the certificate of that encryption unless the connection
		// string trusts it. Only the URL form's encrypt=disable skips TLS.
		t.Run("is down when encryption is off, the certificate is not trusted and the login is encrypted", func(t *testing.T) {
			result := run(t, plainADO("Encrypt=false"), "")

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Contains(t, result.Message, "connection failed: TLS Handshake failed: ")
			assert.Contains(t, result.Message, "x509: ")
		})

		t.Run("is up when the URL form turns TLS off", func(t *testing.T) {
			result := run(t, plainURL(url.Values{"encrypt": {"disable"}}), "")

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		})
	})

	t.Run("password with ; = { and }", func(t *testing.T) {
		require.Equal(t, "Pe;e=K{ing}9z", srv.Password, "the test is about these characters")

		t.Run("reaches the server in the Server= form", func(t *testing.T) {
			result := run(t, adoString(srv, "master", srv.Username, bracedValue(srv.Password), unverifiedTLS), "")

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		})

		t.Run("reaches the server in the sqlserver:// form", func(t *testing.T) {
			result := run(t, urlString(srv, "master", srv.Username, srv.Password, noTLS), "")

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		})
	})
}
