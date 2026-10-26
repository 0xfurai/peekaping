//go:build integration

package testservice

import (
	"context"
	"database/sql"
	"net"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	mysqlURLEnv = "PEE_TEST_MYSQL_URL"

	mysqlImage = "mysql:8.4"
)

var sharedMySQL lazy[SQLDatabase]

// MySQL returns the MySQL for this test: the server in PEE_TEST_MYSQL_URL, or
// a container shared by the tests of the binary and removed when it exits. The
// server has TLS on with a self-signed certificate, and its user has a
// password with reserved characters.
//
// The URL is the connection string the executor takes, with the password
// percent-encoded: mysql://user:password@host:port/database.
func MySQL(t testing.TB) SQLDatabase {
	t.Helper()
	return resolve(t, "MySQL", []string{mysqlURLEnv}, "mysql://user:password@host:port/database, the password percent-encoded",
		func(values []string) (SQLDatabase, error) { return parseSQLURL(values[0], "mysql") }, &sharedMySQL, startMySQL)
}

func startMySQL(ctx context.Context) (SQLDatabase, error) {
	// MySQL 8.4 turns TLS on by itself, with a self-signed certificate it
	// generates at the first start.
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        mysqlImage,
			ExposedPorts: []string{"3306/tcp"},
			Env: map[string]string{
				"MYSQL_RANDOM_ROOT_PASSWORD": "yes",
				"MYSQL_USER":                 sqlUser,
				"MYSQL_PASSWORD":             sqlPassword,
				"MYSQL_DATABASE":             sqlDatabase,
			},
			WaitingFor: wait.ForListeningPort("3306/tcp"),
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
	port, err := c.MappedPort(ctx, "3306/tcp")
	if err != nil {
		return SQLDatabase{}, err
	}
	srv := SQLDatabase{Scheme: "mysql", Addr: net.JoinHostPort(host, port.Port()), User: sqlUser, Password: sqlPassword, Database: sqlDatabase}

	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd, cfg.Net, cfg.Addr, cfg.DBName = srv.User, srv.Password, "tcp", srv.Addr, srv.Database
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return SQLDatabase{}, err
	}
	defer db.Close()
	if err := waitForLogin(ctx, "MySQL", db); err != nil {
		return SQLDatabase{}, err
	}
	return srv, nil
}
