package api_key_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/api_key"
	"peekaping/internal/modules/workspace"

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
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "api-keys.db")}, zap.NewNop().Sugar())
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
	schema := "apikeys_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	return db
}

// forEachBackend runs fn against a migrated SQLite and PostgreSQL database.
func forEachBackend(t *testing.T, fn func(t *testing.T, db *bun.DB)) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(context.Background()))
			_, err := migrator.Migrate(context.Background())
			require.NoError(t, err)
			fn(t, db)
		})
	}
}

// newWorkspace adds a workspace to the database, which an API key must belong to, and returns a
// context for an admin of it
func newWorkspace(t *testing.T, db *bun.DB) (workspace.ID, context.Context) {
	t.Helper()
	id := workspace.ID(uuid.NewString())
	_, err := db.Exec("INSERT INTO workspaces (id, name) VALUES (?, 'Test')", string(id))
	require.NoError(t, err)
	return id, workspace.WithContext(context.Background(), id, workspace.RoleAdmin)
}

// concurrently runs fn n times at once and returns how many of the calls returned true.
func concurrently(n int, fn func() bool) int {
	var accepted atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if fn() {
				accepted.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	return int(accepted.Load())
}

// A key with a usage limit is never accepted more times than the limit, however
// many requests arrive at once.
func TestService_ValidateKey_NeverAcceptsAKeyMoreOftenThanItsLimit(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		workspaceID, ctx := newWorkspace(t, db)
		const limit, requests = 5, 20
		svc := api_key.NewService(api_key.NewSQLRepository(db), zap.NewNop().Sugar())
		maxUsage := int64(limit)
		created, err := svc.Create(ctx, &api_key.CreateRequest{Name: "Limited", MaxUsageCount: &maxUsage})
		require.NoError(t, err)

		accepted := concurrently(requests, func() bool {
			_, err := svc.ValidateKey(ctx, created.Token)
			return err == nil
		})

		assert.Equal(t, limit, accepted)
		stored, err := svc.FindByID(ctx, workspaceID, created.ID)
		require.NoError(t, err)
		assert.EqualValues(t, limit, stored.UsageCount)
	})
}

func TestRepository_ConsumeUsage(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		workspaceID, ctx := newWorkspace(t, db)
		repo := api_key.NewSQLRepository(db)
		newKey := func(maxUsage *int64) string {
			created, err := repo.Create(ctx, &api_key.CreateModel{WorkspaceID: workspaceID, Name: "Key", KeyHash: "sha256$", DisplayKey: "pk_key...", MaxUsageCount: maxUsage})
			require.NoError(t, err)
			return created.ID
		}
		consume := func(id string) bool {
			consumed, err := repo.ConsumeUsage(ctx, id)
			require.NoError(t, err)
			return consumed
		}

		t.Run("counts uses up to the limit", func(t *testing.T) {
			two := int64(2)
			id := newKey(&two)

			assert.True(t, consume(id))
			assert.True(t, consume(id))
			assert.False(t, consume(id), "the limit is reached")

			stored, err := repo.FindByID(ctx, workspaceID, id)
			require.NoError(t, err)
			assert.EqualValues(t, 2, stored.UsageCount)
			assert.NotNil(t, stored.LastUsed)
		})

		t.Run("counts every use of a key with no limit", func(t *testing.T) {
			id := newKey(nil)

			for range 3 {
				assert.True(t, consume(id))
			}

			stored, err := repo.FindByID(ctx, workspaceID, id)
			require.NoError(t, err)
			assert.EqualValues(t, 3, stored.UsageCount)
		})

		t.Run("counts nothing for a key that doesn't exist", func(t *testing.T) {
			assert.False(t, consume(uuid.NewString()))
		})

		t.Run("takes the last use once under concurrent calls", func(t *testing.T) {
			five := int64(5)
			id := newKey(&five)

			consumed := concurrently(20, func() bool {
				ok, err := repo.ConsumeUsage(ctx, id)
				return err == nil && ok
			})

			assert.Equal(t, 5, consumed)
			stored, err := repo.FindByID(ctx, workspaceID, id)
			require.NoError(t, err)
			assert.EqualValues(t, 5, stored.UsageCount)
		})
	})
}
