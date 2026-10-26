package main

import (
	"net/url"
	"os"
	"testing"

	"peekaping/internal/config"

	"github.com/stretchr/testify/require"
)

// Use the same TLS-enabled server as the application's integration check, so a
// migration cannot silently use a different transport from the running services.
func TestConnectToDatabasePostgresSSLModeTLS(t *testing.T) {
	dsn := os.Getenv("PEE_TEST_POSTGRES_TLS_DSN")
	if dsn == "" {
		t.Skip("set PEE_TEST_POSTGRES_TLS_DSN to run PostgreSQL TLS integration checks")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	password, _ := u.User.Password()

	for _, mode := range []string{"", "disable", "allow", "prefer", "require", "verify-ca", "verify-full"} {
		t.Run("mode="+mode, func(t *testing.T) {
			t.Setenv("DB_TYPE", "postgres")
			t.Setenv("DB_HOST", u.Hostname())
			t.Setenv("DB_PORT", u.Port())
			t.Setenv("DB_USER", u.User.Username())
			t.Setenv("DB_PASS", password)
			t.Setenv("DB_NAME", u.Path[1:])
			t.Setenv("DB_SSLMODE", mode)
			cfg, err := config.LoadConfig[config.DBConfig](t.TempDir())
			require.NoError(t, err)
			if mode == "" {
				// Configs built in code have no environment defaults.
				cfg = config.DBConfig{
					DBType: cfg.DBType, DBHost: cfg.DBHost, DBPort: cfg.DBPort,
					DBUser: cfg.DBUser, DBPass: cfg.DBPass, DBName: cfg.DBName,
				}
			}

			db, err := connectToDatabase(&cfg)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })

			var encrypted bool
			err = db.NewRaw("SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid()").Scan(t.Context(), &encrypted)
			require.NoError(t, err)
			require.Equal(t, mode != "" && mode != "disable", encrypted, "DB_SSLMODE did not reach the migration connection")

			var searchPath string
			err = db.NewRaw("SHOW search_path").Scan(t.Context(), &searchPath)
			require.NoError(t, err)
			require.Equal(t, "public", searchPath)
		})
	}
}
