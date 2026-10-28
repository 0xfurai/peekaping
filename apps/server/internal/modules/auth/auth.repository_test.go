package auth_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/setting"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// sqliteDB opens a SQLite database the way the app does, with one connection.
func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "users.db")}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

// postgresDB opens an empty schema in the database at PEE_TEST_POSTGRES_DSN.
func postgresDB(t *testing.T) *bun.DB {
	t.Helper()
	dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PEE_TEST_POSTGRES_DSN to run the PostgreSQL tests")
	}
	admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	schema := "users_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := admin.Exec("CREATE SCHEMA ?", bun.Ident(schema))
	require.NoError(t, err)
	connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn), pgdriver.WithConnParams(map[string]any{"search_path": schema}))
	db := bun.NewDB(sql.OpenDB(connector), pgdialect.New())
	t.Cleanup(func() {
		require.NoError(t, db.Close())
		_, err := admin.Exec("DROP SCHEMA ? CASCADE", bun.Ident(schema))
		require.NoError(t, err)
		require.NoError(t, admin.Close())
	})

	// Open the connections up front and keep them, so that calls made together
	// reach the database together instead of queueing behind new connections
	const connections = 20
	db.SetMaxIdleConns(connections)
	var conns []bun.Conn
	for range connections {
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		conns = append(conns, conn)
	}
	for _, conn := range conns {
		require.NoError(t, conn.Close())
	}
	return db
}

// forEachBackend runs fn against a repository on a migrated SQLite and PostgreSQL
// database.
func forEachBackend(t *testing.T, fn func(t *testing.T, repo auth.Repository)) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(context.Background()))
			_, err := migrator.Migrate(context.Background())
			require.NoError(t, err)
			fn(t, auth.NewSQLRepository(db))
		})
	}
}

func newUser(email string) *auth.Model {
	return &auth.Model{Email: email, Password: "password-hash", Active: true}
}

func TestRepository_CreateFirst_CreatesOnlyTheFirstUser(t *testing.T) {
	forEachBackend(t, func(t *testing.T, repo auth.Repository) {
		ctx := context.Background()

		created, err := repo.CreateFirst(ctx, newUser("first@example.com"))
		require.NoError(t, err)
		assert.NotEmpty(t, created.ID)
		assert.Equal(t, "first@example.com", created.Email)
		assert.True(t, created.Active)
		assert.True(t, created.InstanceAdmin, "the first user manages the install")

		stored, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "first@example.com", stored.Email)
		assert.Equal(t, "password-hash", stored.Password)
		assert.True(t, stored.Active)
		assert.True(t, stored.InstanceAdmin)

		for _, email := range []string{"second@example.com", "first@example.com"} {
			second, err := repo.CreateFirst(ctx, newUser(email))
			assert.ErrorIs(t, err, auth.ErrAdminExists, email)
			assert.Nil(t, second)
		}
		count, err := repo.FindAllCount(ctx)
		require.NoError(t, err)
		assert.EqualValues(t, 1, count)
	})
}

func TestRepository_CreateFirst_ConcurrentCallsCreateOneUser(t *testing.T) {
	forEachBackend(t, func(t *testing.T, repo auth.Repository) {
		ctx := context.Background()
		const calls = 10

		errs := make(chan error, calls)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for n := range calls {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := repo.CreateFirst(ctx, newUser(fmt.Sprintf("admin%d@example.com", n)))
				errs <- err
			}()
		}
		close(start)
		wg.Wait()
		close(errs)

		created := 0
		for err := range errs {
			if err == nil {
				created++
				continue
			}
			assert.ErrorIs(t, err, auth.ErrAdminExists)
		}
		assert.Equal(t, 1, created, "calls that created a user")
		count, err := repo.FindAllCount(ctx)
		require.NoError(t, err)
		assert.EqualValues(t, 1, count)
	})
}

// The first signup writes the user and its workspace in one transaction. Signups that arrive
// together still make one user, one workspace and one membership, on both databases.
func TestService_Register_ConcurrentSignupsMakeOneUserAndOneWorkspace(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := open(t)
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(ctx))
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			logger := zap.NewNop().Sugar()
			settings := setting.NewService(setting.NewSQLRepository(db), logger)
			require.NoError(t, settings.InitializeSettings(ctx))
			svc := newService(db, auth.NewSQLRepository(db), auth.NewTokenMaker(settings, logger), logger)
			const calls = 10

			errs := make(chan error, calls)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for n := range calls {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					_, err := svc.Register(ctx, auth.RegisterDto{Email: fmt.Sprintf("admin%d@example.com", n), Password: "Correct-horse-1"})
					errs <- err
				}()
			}
			close(start)
			wg.Wait()
			close(errs)

			created := 0
			for err := range errs {
				if err == nil {
					created++
					continue
				}
				assert.ErrorIs(t, err, auth.ErrAdminExists)
			}
			assert.Equal(t, 1, created, "signups that created a user")
			for table, want := range map[string]int{"users": 1, "workspaces": 1, "workspace_members": 1} {
				n, err := db.NewSelect().Table(table).Count(ctx)
				require.NoError(t, err)
				assert.Equal(t, want, n, table)
			}
		})
	}
}

func TestRepository_FindByEmailIgnoringCase_FindsTheAccountInAnyCaseAroundSpaces(t *testing.T) {
	forEachBackend(t, func(t *testing.T, repo auth.Repository) {
		ctx := context.Background()
		created, err := repo.Create(ctx, newUser("Alice@Example.com"))
		require.NoError(t, err)

		for _, typed := range []string{"Alice@Example.com", "alice@example.com", "ALICE@EXAMPLE.COM", "  alice@example.com "} {
			found, err := repo.FindByEmailIgnoringCase(ctx, typed)
			require.NoError(t, err, typed)
			require.NotNil(t, found, typed)
			assert.Equal(t, created.ID, found.ID, typed)
		}
		found, err := repo.FindByEmailIgnoringCase(ctx, "bob@example.com")
		require.NoError(t, err)
		assert.Nil(t, found)
		exact, err := repo.FindByEmail(ctx, "alice@example.com")
		require.NoError(t, err)
		assert.Nil(t, exact, "FindByEmail, which login uses, stays exact")
	})
}
