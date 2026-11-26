//go:build integration

package testservice

import (
	"context"
	"database/sql"
	"net"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/uptrace/bun/driver/pgdriver"
)

const (
	// Not PEE_TEST_POSTGRES_DSN: the repository tests use that one for a
	// different server, an empty database they create their tables in.
	postgresURLEnv = "PEE_TEST_POSTGRES_EXECUTOR_URL"

	postgresImage = "postgres:17"
)

var sharedPostgres lazy[SQLDatabase]

// Postgres returns the PostgreSQL for this test: the server in
// PEE_TEST_POSTGRES_EXECUTOR_URL, or a container shared by the tests of the
// binary and removed when it exits. The server has TLS on with a self-signed
// certificate, and its user has a password with reserved characters.
//
// The URL is the connection string the executor takes, with the password
// percent-encoded: postgres://user:password@host:port/database.
func Postgres(t testing.TB) SQLDatabase {
	t.Helper()
	return resolve(t, "PostgreSQL", []string{postgresURLEnv}, "postgres://user:password@host:port/database, the password percent-encoded",
		func(values []string) (SQLDatabase, error) { return parseSQLURL(values[0], "postgres") }, &sharedPostgres, startPostgres)
}

func startPostgres(ctx context.Context) (SQLDatabase, error) {
	// The Debian image ships a self-signed certificate (the "snakeoil" one),
	// which the server uses once told where it is. The compose file starts the
	// same server with the same flags.
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        postgresImage,
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER":     sqlUser,
				"POSTGRES_PASSWORD": sqlPassword,
				"POSTGRES_DB":       sqlDatabase,
			},
			Cmd: []string{
				"-c", "ssl=on",
				"-c", "ssl_cert_file=/etc/ssl/certs/ssl-cert-snakeoil.pem",
				"-c", "ssl_key_file=/etc/ssl/private/ssl-cert-snakeoil.key",
			},
			WaitingFor: wait.ForListeningPort("5432/tcp"),
		},
		Started: true,
	})
	if err != nil {
		return SQLDatabase{}, err
	}

	host, err := c.Host(ctx)
	if err != nil {
		return SQLDatabase{}, err
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return SQLDatabase{}, err
	}
	srv := SQLDatabase{Scheme: "postgres", Addr: net.JoinHostPort(host, port.Port()), User: sqlUser, Password: sqlPassword, Database: sqlDatabase}

	db := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(srv.URL(srv.Password, "sslmode=require"))))
	defer db.Close()
	if err := waitForLogin(ctx, "PostgreSQL", db); err != nil {
		return SQLDatabase{}, err
	}
	return srv, nil
}
