package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/mongoimport"
	"peekaping/internal/mongoimport/mongoimporttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
)

func runImportMongo(args ...string) error {
	app := &cli.App{Name: "bun", Commands: []*cli.Command{newDBCommand()}}
	return app.Run(append([]string{"bun", "db", "import-mongo"}, args...))
}

func TestImportMongoCommandCopiesIntoTheDatabaseTheEnvironmentNames(t *testing.T) {
	in := mongoimporttest.Seed(t)
	path := filepath.Join(t.TempDir(), "peekaping.db")
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", path)

	require.NoError(t, runImportMongo("--mongo-uri", in.URI, "--mongo-db", in.Database))

	sqlDB, err := sql.Open(sqliteshim.ShimName, path)
	require.NoError(t, err)
	db := bun.NewDB(sqlDB, sqlitedialect.New())
	defer db.Close()
	for table, want := range map[string]int{"users": 2, "monitors": 5, "heartbeats": 7, "stat_minutely": 7, "status_pages": 2} {
		n, err := db.NewSelect().TableExpr("?", bun.Ident(table)).Count(context.Background())
		require.NoError(t, err, table)
		assert.Equal(t, want, n, table)
	}

	// The database is no longer empty, so a second run changes nothing and says why
	err = runImportMongo("--mongo-uri", in.URI, "--mongo-db", in.Database)
	assert.ErrorIs(t, err, mongoimport.ErrTargetNotEmpty)
}

func TestImportMongoCommandPostgresSSLMode(t *testing.T) {
	in := mongoimporttest.Seed(t)
	for _, mode := range []string{"", "disable", "require"} {
		t.Run("mode="+mode, func(t *testing.T) {
			cfg := postgresConfig(t)
			probe, err := connectToDatabase(cfg)
			require.NoError(t, err)
			var serverSSL string
			err = probe.NewRaw("SHOW ssl").Scan(t.Context(), &serverSSL)
			require.NoError(t, probe.Close())
			require.NoError(t, err)

			t.Setenv("DB_TYPE", cfg.DBType)
			t.Setenv("DB_HOST", cfg.DBHost)
			t.Setenv("DB_PORT", cfg.DBPort)
			t.Setenv("DB_USER", cfg.DBUser)
			t.Setenv("DB_PASS", cfg.DBPass)
			t.Setenv("DB_NAME", cfg.DBName)
			t.Setenv("DB_SSLMODE", mode)
			err = runImportMongo("--mongo-uri", in.URI, "--mongo-db", in.Database)
			if mode == "require" && serverSSL == "off" {
				require.ErrorContains(t, err, "SSL")
			} else {
				require.NoError(t, err)
			}

			// The application must use the same transport as the command that imported its data.
			db, err := infra.ProvideSQLDB(&config.Config{
				DBType: cfg.DBType, DBHost: cfg.DBHost, DBPort: cfg.DBPort,
				DBUser: cfg.DBUser, DBPass: cfg.DBPass, DBName: cfg.DBName, DBSSLMode: mode,
			}, zap.NewNop().Sugar())
			if mode == "require" && serverSSL == "off" {
				require.ErrorContains(t, err, "SSL")
				return
			}
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			var encrypted bool
			err = db.NewRaw("SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid()").Scan(t.Context(), &encrypted)
			require.NoError(t, err)
			require.Equal(t, mode == "require", encrypted)
			n, err := db.NewSelect().Table("heartbeats").Count(t.Context())
			require.NoError(t, err)
			require.Equal(t, 7, n)
		})
	}
}

func TestImportMongoCommandTakesTheDatabaseFromTheURI(t *testing.T) {
	in := mongoimporttest.Seed(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", filepath.Join(t.TempDir(), "peekaping.db"))
	require.NoError(t, runImportMongo("--mongo-uri", in.URI+"/"+in.Database))
}

// The URI may carry the password of the MongoDB, so it can come from the environment
// and stay out of the command line and the shell history. The database flag wins over
// the database in the URI, which is the fallback.
func TestImportMongoCommandReadsTheMongoSettingsFromTheEnvironment(t *testing.T) {
	in := mongoimporttest.Seed(t)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", filepath.Join(t.TempDir(), "peekaping.db"))
	t.Setenv("MONGO_URI", in.URI)
	t.Setenv("MONGO_DB", in.Database)
	require.NoError(t, runImportMongo())

	// --mongo-db wins over the database in the URI, so a database with no install in it is refused
	t.Setenv("DB_NAME", filepath.Join(t.TempDir(), "other.db"))
	t.Setenv("MONGO_DB", "")
	t.Setenv("MONGO_URI", in.URI+"/"+in.Database)
	require.NoError(t, runImportMongo())
	t.Setenv("DB_NAME", filepath.Join(t.TempDir(), "third.db"))
	assert.ErrorContains(t, runImportMongo("--mongo-db", in.Database+"_typo"), "has no collections")
}

func TestImportMongoCommandNeedsAMongoURI(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", filepath.Join(t.TempDir(), "peekaping.db"))
	assert.ErrorContains(t, runImportMongo(), "mongo-uri")
}

// DB_TYPE=mongo is the install being moved, not the place to move it to
func TestImportMongoCommandWritesToSQLOnly(t *testing.T) {
	t.Setenv("DB_TYPE", "mongo")
	t.Setenv("DB_NAME", "peekaping")
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "27017")
	t.Setenv("DB_USER", "peekaping")
	t.Setenv("DB_PASS", "password")
	err := runImportMongo("--mongo-uri", "mongodb://localhost:27017")
	assert.ErrorContains(t, err, "set DB_TYPE to sqlite or postgres")
}
