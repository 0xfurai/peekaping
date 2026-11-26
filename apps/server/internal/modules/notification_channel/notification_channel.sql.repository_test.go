package notification_channel_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacetest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// sqliteDB opens a SQLite database the way the app does, with foreign keys on.
func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "notification_channel.db")}, zap.NewNop().Sugar())
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
	schema := "notification_channel_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

// migrateDB migrates the database as the startup script does with cmd/bun.
func migrateDB(t *testing.T, db *bun.DB) {
	t.Helper()
	ctx := context.Background()
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err := migrator.Migrate(ctx)
	require.NoError(t, err)
}

// forEachSQLBackend runs fn on a migrated database of every SQL storage backend. PostgreSQL runs
// when PEE_TEST_POSTGRES_DSN is set.
func forEachSQLBackend(t *testing.T, fn func(t *testing.T, db *bun.DB)) {
	backends := []struct {
		name string
		open func(*testing.T) *bun.DB
	}{
		{"SQLite", sqliteDB},
		{"Postgres", postgresDB},
	}
	for _, backend := range backends {
		t.Run(backend.name, func(t *testing.T) {
			db := backend.open(t)
			migrateDB(t, db)
			fn(t, db)
		})
	}
}

func TestSQLRepositoryUpdatePartialKeepsOmittedFields(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := notification_channel.NewSQLRepository(db)
		ws := workspace.ID(workspacetest.OldestOrAdd(t, db))

		channelConfig := `{"webhook_url":"https://hooks.example.com/services/T000/B000/XXXX"}`
		created, err := repo.Create(ctx, ws, &notification_channel.Model{
			Name:      "Ops pager (EU)",
			Type:      "webhook",
			Active:    true,
			IsDefault: true,
			Config:    &channelConfig,
		})
		require.NoError(t, err)
		saved, err := repo.FindByID(ctx, ws, created.ID)
		require.NoError(t, err)
		require.NotNil(t, saved)
		require.True(t, saved.Active, "the channel starts active, so switching it off is a real change")

		inactive := false
		require.NoError(t, repo.UpdatePartial(ctx, ws, created.ID, &notification_channel.UpdateModel{Active: &inactive}))

		got, err := repo.FindByID(ctx, ws, created.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.False(t, got.Active)
		assert.Equal(t, "Ops pager (EU)", got.Name)
		assert.Equal(t, "webhook", got.Type)
		assert.True(t, got.IsDefault)
		if assert.NotNil(t, got.Config) {
			// PostgreSQL stores the config as JSONB, which rewrites the spacing.
			assert.JSONEq(t, `{"webhook_url":"https://hooks.example.com/services/T000/B000/XXXX"}`, *got.Config)
		}
	})
}

func TestSQLRepositoryScopesEveryQueryToTheWorkspace(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := notification_channel.NewSQLRepository(db)
		mine := workspace.ID(workspacetest.OldestOrAdd(t, db))
		workspacetest.Add(t, db, workspacetest.Other, "Other")
		config := `{"webhook_url":"https://hooks.example.com/a"}`
		channel, err := repo.Create(ctx, mine, &notification_channel.Model{Name: "Mine", Type: "webhook", Active: true, Config: &config, WorkspaceID: workspacetest.Other})
		require.NoError(t, err)
		assert.Equal(t, mine, channel.WorkspaceID, "the workspace is the argument's, whatever the model says")
		theirs, err := repo.Create(ctx, workspacetest.Other, &notification_channel.Model{Name: "Theirs", Type: "webhook", Active: true, Config: &config})
		require.NoError(t, err)

		found, err := repo.FindByID(ctx, workspacetest.Other, channel.ID)
		require.NoError(t, err)
		assert.Nil(t, found)
		anywhere, err := repo.FindByIDAnyWorkspace(ctx, channel.ID)
		require.NoError(t, err)
		require.NotNil(t, anywhere)
		assert.Equal(t, mine, anywhere.WorkspaceID, "the background paths read it with its workspace")
		listed, err := repo.FindAll(ctx, mine, 0, 10, "")
		require.NoError(t, err)
		require.Len(t, listed, 1)
		assert.Equal(t, channel.ID, listed[0].ID)
		searched, err := repo.FindAll(ctx, mine, 0, 10, "theirs")
		require.NoError(t, err)
		assert.Empty(t, searched)
		all, err := repo.FindAllAnyWorkspace(ctx, 0, 10)
		require.NoError(t, err)
		assert.Len(t, all, 2)

		rename := "Hijacked"
		assert.ErrorIs(t, repo.UpdateFull(ctx, workspacetest.Other, channel.ID, &notification_channel.Model{Name: rename, Type: "webhook", Config: &config}), notification_channel.ErrNotFound)
		assert.ErrorIs(t, repo.UpdatePartial(ctx, workspacetest.Other, channel.ID, &notification_channel.UpdateModel{Name: &rename}), notification_channel.ErrNotFound)
		assert.ErrorIs(t, repo.UpdatePartial(ctx, workspacetest.Other, channel.ID, &notification_channel.UpdateModel{}), notification_channel.ErrNotFound)
		assert.ErrorIs(t, repo.Delete(ctx, workspacetest.Other, channel.ID), notification_channel.ErrNotFound)
		assert.ErrorIs(t, repo.Delete(ctx, mine, uuid.NewString()), notification_channel.ErrNotFound)

		kept, err := repo.FindByID(ctx, mine, channel.ID)
		require.NoError(t, err)
		require.NotNil(t, kept)
		assert.Equal(t, "Mine", kept.Name, "the channel is as it was")
		other, err := repo.FindByID(ctx, workspacetest.Other, theirs.ID)
		require.NoError(t, err)
		assert.Equal(t, "Theirs", other.Name)

		require.NoError(t, repo.UpdateFull(ctx, mine, channel.ID, &notification_channel.Model{Name: "Renamed", Type: "webhook", Active: true, Config: &config, WorkspaceID: workspacetest.Other}))
		renamed, err := repo.FindByID(ctx, mine, channel.ID)
		require.NoError(t, err)
		assert.Equal(t, "Renamed", renamed.Name)
		assert.Equal(t, mine, renamed.WorkspaceID, "an update can't move a channel to another workspace")
		require.NoError(t, repo.Delete(ctx, mine, channel.ID))
	})
}
