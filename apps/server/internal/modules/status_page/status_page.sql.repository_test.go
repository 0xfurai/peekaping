package status_page_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacetest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/migrate"
)

// onEachDatabase runs the test on a migrated SQLite database, and on a PostgreSQL schema of its
// own when PEE_TEST_POSTGRES_DSN names a server
func onEachDatabase(t *testing.T, test func(t *testing.T, repo status_page.Repository, workspaceID, otherWorkspaceID workspace.ID)) {
	t.Helper()
	run := func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		migrator := migrate.NewMigrator(db, migrations.Migrations)
		require.NoError(t, migrator.Init(ctx))
		_, err := migrator.Migrate(ctx)
		require.NoError(t, err)
		workspaceID := workspace.ID(workspacetest.OldestOrAdd(t, db))
		workspacetest.Add(t, db, workspacetest.Other, "Other")
		test(t, status_page.NewSQLRepository(db), workspaceID, workspacetest.Other)
	}

	t.Run("SQLite", func(t *testing.T) {
		sqlDB, err := sql.Open(sqliteshim.ShimName, filepath.Join(t.TempDir(), "status_page.db"))
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(1) // as infra.NewSQL does: one connection, so writes queue instead of failing as locked
		run(t, bun.NewDB(sqlDB, sqlitedialect.New()))
	})
	t.Run("Postgres", func(t *testing.T) {
		dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("set PEE_TEST_POSTGRES_DSN to run PostgreSQL integration checks")
		}
		admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
		schema := "pee389_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		_, err := admin.Exec("CREATE SCHEMA ?", bun.Ident(schema))
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := admin.Exec("DROP SCHEMA ? CASCADE", bun.Ident(schema))
			require.NoError(t, err)
			require.NoError(t, admin.Close())
		})
		connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn), pgdriver.WithConnParams(map[string]interface{}{"search_path": schema}))
		run(t, bun.NewDB(sql.OpenDB(connector), pgdialect.New()))
	})
}

// basePage is a page with every column set
func basePage() *status_page.Model {
	return &status_page.Model{
		Slug: "base", Title: "Base", Description: "Base description", Icon: "base-icon", Theme: "light",
		Published: true, ShowLocations: true, FooterText: "Base footer", AutoRefreshInterval: 60,
	}
}

// An update changes the columns it names and no other, and the page read back says so.
func TestStatusPageSQLRepository_UpdateChangesOnlyTheColumnsItNames(t *testing.T) {
	onEachDatabase(t, func(t *testing.T, repo status_page.Repository, workspaceID, _ workspace.ID) {
		ctx := context.Background()

		for _, tt := range []struct {
			name   string
			patch  status_page.UpdateModel
			change func(m *status_page.Model)
		}{
			{"title", status_page.UpdateModel{Title: ptr("New title")}, func(m *status_page.Model) { m.Title = "New title" }},
			{"empty title", status_page.UpdateModel{Title: ptr("")}, func(m *status_page.Model) { m.Title = "" }},
			{"description", status_page.UpdateModel{Description: ptr("New description")}, func(m *status_page.Model) { m.Description = "New description" }},
			{"empty description", status_page.UpdateModel{Description: ptr("")}, func(m *status_page.Model) { m.Description = "" }},
			{"slug", status_page.UpdateModel{Slug: ptr("renamed")}, func(m *status_page.Model) { m.Slug = "renamed" }},
			{"icon", status_page.UpdateModel{Icon: ptr("new-icon")}, func(m *status_page.Model) { m.Icon = "new-icon" }},
			{"theme", status_page.UpdateModel{Theme: ptr("dark")}, func(m *status_page.Model) { m.Theme = "dark" }},
			{"unpublished", status_page.UpdateModel{Published: ptr(false)}, func(m *status_page.Model) { m.Published = false }},
			{"locations hidden", status_page.UpdateModel{ShowLocations: ptr(false)}, func(m *status_page.Model) { m.ShowLocations = false }},
			{"footer text", status_page.UpdateModel{FooterText: ptr("New footer")}, func(m *status_page.Model) { m.FooterText = "New footer" }},
			{"empty footer text", status_page.UpdateModel{FooterText: ptr("")}, func(m *status_page.Model) { m.FooterText = "" }},
			{"auto refresh interval", status_page.UpdateModel{AutoRefreshInterval: ptr(15)}, func(m *status_page.Model) { m.AutoRefreshInterval = 15 }},
			{"zero auto refresh interval", status_page.UpdateModel{AutoRefreshInterval: ptr(0)}, func(m *status_page.Model) { m.AutoRefreshInterval = 0 }},
			{"every column", status_page.UpdateModel{
				Slug: ptr("all"), Title: ptr("T"), Description: ptr("D"), Icon: ptr("I"), Theme: ptr("dark"),
				Published: ptr(false), ShowLocations: ptr(false), FooterText: ptr("F"), AutoRefreshInterval: ptr(5),
			}, func(m *status_page.Model) {
				*m = status_page.Model{
					Slug: "all", Title: "T", Description: "D", Icon: "I", Theme: "dark",
					Published: false, ShowLocations: false, FooterText: "F", AutoRefreshInterval: 5,
				}
			}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				page := basePage()
				page.Slug = "base-" + uuid.NewString()[:8]
				created, err := repo.Create(ctx, workspaceID, page)
				require.NoError(t, err)
				before, err := repo.FindByID(ctx, workspaceID, created.ID)
				require.NoError(t, err)
				time.Sleep(20 * time.Millisecond)

				patch := tt.patch
				require.NoError(t, repo.Update(ctx, workspaceID, created.ID, &patch))

				after, err := repo.FindByID(ctx, workspaceID, created.ID)
				require.NoError(t, err)
				want := *page
				tt.change(&want)
				if patch.Slug == nil {
					want.Slug = page.Slug
				}
				want.ID, want.WorkspaceID, want.CreatedAt, want.UpdatedAt = before.ID, workspaceID, before.CreatedAt, after.UpdatedAt
				assert.Equal(t, &want, after)
				assert.True(t, after.UpdatedAt.After(before.UpdatedAt), "a write moves updated_at")
			})
		}
	})
}

// An update that names nothing writes nothing, and still says whether the page is there.
func TestStatusPageSQLRepository_UpdateWithNothingToChange(t *testing.T) {
	onEachDatabase(t, func(t *testing.T, repo status_page.Repository, workspaceID, otherWorkspaceID workspace.ID) {
		ctx := context.Background()
		created, err := repo.Create(ctx, workspaceID, basePage())
		require.NoError(t, err)
		before, err := repo.FindByID(ctx, workspaceID, created.ID)
		require.NoError(t, err)
		time.Sleep(20 * time.Millisecond)

		require.NoError(t, repo.Update(ctx, workspaceID, created.ID, &status_page.UpdateModel{}))
		after, err := repo.FindByID(ctx, workspaceID, created.ID)
		require.NoError(t, err)
		assert.Equal(t, before, after, "no write moves updated_at")

		assert.ErrorIs(t, repo.Update(ctx, workspaceID, uuid.NewString(), &status_page.UpdateModel{}), status_page.ErrNotFound)
		assert.ErrorIs(t, repo.Update(ctx, otherWorkspaceID, created.ID, &status_page.UpdateModel{}), status_page.ErrNotFound)
	})
}

// An update of a page that isn't in the workspace is not found, and changes nothing.
func TestStatusPageSQLRepository_UpdateOfAPageThatIsNotThere(t *testing.T) {
	onEachDatabase(t, func(t *testing.T, repo status_page.Repository, workspaceID, otherWorkspaceID workspace.ID) {
		ctx := context.Background()
		created, err := repo.Create(ctx, workspaceID, basePage())
		require.NoError(t, err)
		before, err := repo.FindByID(ctx, workspaceID, created.ID)
		require.NoError(t, err)

		assert.ErrorIs(t, repo.Update(ctx, workspaceID, uuid.NewString(), &status_page.UpdateModel{Title: ptr("Nobody")}), status_page.ErrNotFound)
		assert.ErrorIs(t, repo.Update(ctx, otherWorkspaceID, created.ID, &status_page.UpdateModel{Title: ptr("Hijacked"), Published: ptr(false)}), status_page.ErrNotFound)

		after, err := repo.FindByID(ctx, workspaceID, created.ID)
		require.NoError(t, err)
		assert.Equal(t, before, after)
	})
}
