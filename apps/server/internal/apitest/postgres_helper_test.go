package apitest

import (
	"database/sql"
	"net/url"
	"testing"
	"time"

	"peekaping/internal/config"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun/driver/pgdriver"
)

// usePostgres points the app at a database of its own on the PostgreSQL server the DSN names
func usePostgres(t *testing.T, dsn string) func(*config.Config) {
	t.Helper()
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	password, _ := parsed.User.Password()
	return func(cfg *config.Config) {
		cfg.DBType, cfg.DBHost, cfg.DBPort = "postgres", parsed.Hostname(), parsed.Port()
		cfg.DBUser, cfg.DBPass = parsed.User.Username(), password
		cfg.DBName = "apitest_" + uuid.NewString()[:8]
		admin := openAdmin(dsn)
		defer admin.Close()
		// A Docker Desktop port forwarding resets the first connection now and then
		var err error
		for range 5 {
			if _, err = admin.Exec("CREATE DATABASE " + cfg.DBName); err == nil {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		require.NoError(t, err)
		// WITH (FORCE) (PostgreSQL 13+, CI runs 17) also ends a connection the app still holds
		t.Cleanup(func() {
			admin := openAdmin(dsn)
			defer admin.Close()
			_, err := admin.Exec("DROP DATABASE IF EXISTS " + cfg.DBName + " WITH (FORCE)")
			require.NoError(t, err)
		})
	}
}

// adminStatementTimeout is how long CREATE DATABASE and DROP DATABASE may take. Both write to disk
// and wait for a checkpoint, which on a slow disk takes longer than the driver's default read
// timeout of 10 seconds.
const adminStatementTimeout = 2 * time.Minute

// openAdmin opens a connection to the server the DSN names, for creating and dropping databases
func openAdmin(dsn string) *sql.DB {
	return sql.OpenDB(pgdriver.NewConnector(
		pgdriver.WithDSN(dsn),
		pgdriver.WithReadTimeout(adminStatementTimeout),
		pgdriver.WithWriteTimeout(adminStatementTimeout),
	))
}
