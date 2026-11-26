//go:build integration

package testservice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"
	"time"

	_ "github.com/denisenkom/go-mssqldb" // Microsoft SQL Server driver
	"github.com/testcontainers/testcontainers-go"
)

const (
	sqlServerURLEnv = "PEE_TEST_SQLSERVER_URL"

	// The sa password is also in docker-compose.integration.yml. It meets
	// SQL Server's complexity rules and holds ";", "=", "{" and "}", the
	// characters a connection string has to escape.
	sqlServerPassword = "Pe;e=K{ing}9z"

	sqlServerImage = "mcr.microsoft.com/mssql/server:2022-latest"

	// sqlServerStartTimeout bounds the wait for the first login. The server
	// takes tens of seconds to start, and minutes under x86 emulation.
	sqlServerStartTimeout = 5 * time.Minute
)

// SQLServerServer is a running Microsoft SQL Server, signed in as the system
// administrator sa. Its certificate is self-signed.
type SQLServerServer struct {
	Host     string
	Port     string
	Username string
	Password string
}

// Addr is host:port.
func (s SQLServerServer) Addr() string { return net.JoinHostPort(s.Host, s.Port) }

var sharedSQLServer lazy[SQLServerServer]

// SQLServer returns the SQL Server for this test: the server in
// PEE_TEST_SQLSERVER_URL (sqlserver://sa:<password>@<host>:<port>, with the
// password percent-encoded), or a container shared by the tests of the binary
// and removed when it exits. The image exists only for x86-64, so on an Arm
// machine Docker runs it under emulation, which is slow.
func SQLServer(t testing.TB) SQLServerServer {
	t.Helper()
	return resolve(t, "SQL Server", []string{sqlServerURLEnv}, "sqlserver://sa:<percent-encoded password>@host:port",
		func(values []string) (SQLServerServer, error) { return parseSQLServerURL(values[0]) }, &sharedSQLServer, startSQLServer)
}

func parseSQLServerURL(raw string) (SQLServerServer, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return SQLServerServer{}, err
	}
	password, _ := u.User.Password()
	if u.Scheme != "sqlserver" || u.Hostname() == "" || u.Port() == "" || u.User.Username() == "" || password == "" {
		return SQLServerServer{}, errors.New("need scheme sqlserver, a user, a password, a host and a port")
	}
	return SQLServerServer{Host: u.Hostname(), Port: u.Port(), Username: u.User.Username(), Password: password}, nil
}

func startSQLServer(ctx context.Context) (SQLServerServer, error) {
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:         sqlServerImage,
			ImagePlatform: "linux/amd64",
			Env:           map[string]string{"ACCEPT_EULA": "Y", "MSSQL_SA_PASSWORD": sqlServerPassword},
			ExposedPorts:  []string{"1433/tcp"},
		},
		Started: true,
	})
	if err != nil {
		return SQLServerServer{}, err
	}
	host, err := c.Host(ctx)
	if err != nil {
		return SQLServerServer{}, err
	}
	port, err := c.MappedPort(ctx, "1433/tcp")
	if err != nil {
		return SQLServerServer{}, err
	}
	srv := SQLServerServer{Host: host, Port: port.Port(), Username: "sa", Password: sqlServerPassword}

	ctx, cancel := context.WithTimeout(ctx, sqlServerStartTimeout)
	defer cancel()
	if err := waitForSQLServer(ctx, srv); err != nil {
		return SQLServerServer{}, err
	}
	return srv, nil
}

// waitForSQLServer returns once sa can run a query. The port opens well before
// the server accepts logins.
func waitForSQLServer(ctx context.Context, srv SQLServerServer) error {
	dsn := (&url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(srv.Username, srv.Password),
		Host:     srv.Addr(),
		RawQuery: url.Values{"database": {"master"}, "encrypt": {"disable"}}.Encode(),
	}).String()
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	var lastErr error
	for ctx.Err() == nil {
		attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
		var one int
		lastErr = db.QueryRowContext(attempt, "SELECT 1").Scan(&one)
		cancel()
		if lastErr == nil {
			return nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("no login on SQL Server: %w", lastErr)
}
