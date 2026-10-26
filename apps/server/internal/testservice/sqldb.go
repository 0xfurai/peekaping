//go:build integration

package testservice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// The account and database of the PostgreSQL and MySQL servers. The password
// holds every character a connection string has to escape, so a test sees
// whether an executor passes it on intact: @ : / % ?. It has no $ or quote,
// which the compose file and the containers' start commands would have to
// escape too. These values are also in docker-compose.integration.yml.
const (
	sqlUser     = "peekaping"
	sqlPassword = "p@ss:w/rd%1?x"
	sqlDatabase = "peekaping"
)

// SQLDatabase is a database on a running PostgreSQL or MySQL server, with the
// account to log in to it. The server has TLS on and a self-signed certificate.
type SQLDatabase struct {
	Scheme   string // "postgres" or "mysql", the scheme of the executor's connection string
	Addr     string // host:port
	User     string
	Password string
	Database string
}

// URL is the connection string an executor takes for this server, with the
// given password instead of the server's (to test a wrong one) and rawQuery,
// such as "sslmode=require", after the "?". User, password and database are
// percent-encoded.
func (s SQLDatabase) URL(password, rawQuery string) string {
	u := url.URL{
		Scheme:   s.Scheme,
		User:     url.UserPassword(s.User, password),
		Host:     s.Addr,
		Path:     "/" + s.Database,
		RawQuery: rawQuery,
	}
	return u.String()
}

// parseSQLURL reads the value of PEE_TEST_POSTGRES_EXECUTOR_URL or
// PEE_TEST_MYSQL_URL: the connection string the executor takes, with the
// password percent-encoded.
func parseSQLURL(raw, scheme string) (SQLDatabase, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return SQLDatabase{}, err
	}
	var username, password string
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}
	database := ""
	if len(u.Path) > 1 {
		database = u.Path[1:]
	}
	if u.Scheme != scheme || u.Hostname() == "" || u.Port() == "" || username == "" || password == "" || database == "" {
		return SQLDatabase{}, fmt.Errorf("need scheme %s, a user, a password, a host, a port and a database", scheme)
	}
	return SQLDatabase{Scheme: scheme, Addr: u.Host, User: username, Password: password, Database: database}, nil
}

// waitForLogin returns once db logs in and answers a query. A container's
// port opens before its server is ready, since the image's init scripts run
// first.
func waitForLogin(ctx context.Context, service string, db *sql.DB) error {
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
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("no login to %s: %w", service, errors.Join(lastErr, ctx.Err()))
}
