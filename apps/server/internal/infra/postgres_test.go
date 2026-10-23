package infra

import (
	"net/url"
	"os"
	"testing"

	"peekaping/internal/config"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The server must support TLS and trust the supplied credentials. Its certificate must
// be trusted by the system CA store, and both TLS and plain connections must be allowed.
func TestProvideSQLDBPostgresSSLModeTLS(t *testing.T) {
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
			cfg, err := config.LoadConfig[config.Config](t.TempDir())
			require.NoError(t, err)
			if mode == "" {
				// Configs built in code have no environment defaults.
				cfg = config.Config{
					DBType: cfg.DBType, DBHost: cfg.DBHost, DBPort: cfg.DBPort,
					DBUser: cfg.DBUser, DBPass: cfg.DBPass, DBName: cfg.DBName,
				}
			}

			db, err := ProvideSQLDB(&cfg, zap.NewNop().Sugar())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })

			var encrypted bool
			err = db.NewRaw("SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid()").Scan(t.Context(), &encrypted)
			require.NoError(t, err)
			require.Equal(t, mode != "" && mode != "disable", encrypted, "DB_SSLMODE did not reach the PostgreSQL connection")
		})
	}
}
