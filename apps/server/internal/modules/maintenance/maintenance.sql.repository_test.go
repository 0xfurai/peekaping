package maintenance_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/maintenance"
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

// onEachDatabase runs the test on a migrated SQLite database, and on a PostgreSQL schema of
// its own when PEE_TEST_POSTGRES_DSN names a server
func onEachDatabase(t *testing.T, test func(t *testing.T, repo maintenance.Repository, workspaceID, otherWorkspaceID workspace.ID)) {
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
		test(t, maintenance.NewSQLRepository(db), workspaceID, workspacetest.Other)
	}

	t.Run("SQLite", func(t *testing.T) {
		sqlDB, err := sql.Open(sqliteshim.ShimName, filepath.Join(t.TempDir(), "maintenance.db"))
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

func ptr[T any](v T) *T { return &v }

// baseWindow is a window with every column set
func baseWindow() *maintenance.Model {
	return &maintenance.Model{
		Title: "Base", Description: "Base description", Active: true, Strategy: "recurring-weekday",
		StartDateTime: ptr("2026-01-01T00:00"), EndDateTime: ptr("2026-12-31T00:00"),
		StartTime: ptr("09:00"), EndTime: ptr("17:00"),
		Weekdays: []int{1, 3}, DaysOfMonth: []int{5, 10}, IntervalDay: ptr(2),
		Cron: ptr("0 9 * * 1,3"), Timezone: ptr("UTC"), Duration: ptr(90),
	}
}

// A partial update changes the columns it names and no other, and the window read back says so.
func TestSQLRepository_UpdatePartialChangesOnlyTheColumnsItNames(t *testing.T) {
	onEachDatabase(t, func(t *testing.T, repo maintenance.Repository, workspaceID, _ workspace.ID) {
		ctx := context.Background()

		for _, tt := range []struct {
			name   string
			patch  maintenance.UpdateModel
			change func(m *maintenance.Model)
		}{
			{"title", maintenance.UpdateModel{Title: ptr("New title")}, func(m *maintenance.Model) { m.Title = "New title" }},
			{"empty title", maintenance.UpdateModel{Title: ptr("")}, func(m *maintenance.Model) { m.Title = "" }},
			{"description", maintenance.UpdateModel{Description: ptr("New description")}, func(m *maintenance.Model) { m.Description = "New description" }},
			{"empty description", maintenance.UpdateModel{Description: ptr("")}, func(m *maintenance.Model) { m.Description = "" }},
			{"paused", maintenance.UpdateModel{Active: ptr(false)}, func(m *maintenance.Model) { m.Active = false }},
			{"strategy", maintenance.UpdateModel{Strategy: ptr("cron")}, func(m *maintenance.Model) { m.Strategy = "cron" }},
			{"start date-time", maintenance.UpdateModel{StartDateTime: ptr("2027-02-03T04:05")}, func(m *maintenance.Model) { m.StartDateTime = ptr("2027-02-03T04:05") }},
			{"end date-time", maintenance.UpdateModel{EndDateTime: ptr("2027-06-07T08:09")}, func(m *maintenance.Model) { m.EndDateTime = ptr("2027-06-07T08:09") }},
			{"start time", maintenance.UpdateModel{StartTime: ptr("01:30")}, func(m *maintenance.Model) { m.StartTime = ptr("01:30") }},
			{"end time", maintenance.UpdateModel{EndTime: ptr("23:45")}, func(m *maintenance.Model) { m.EndTime = ptr("23:45") }},
			{"weekdays", maintenance.UpdateModel{Weekdays: []int{2, 4, 6}}, func(m *maintenance.Model) { m.Weekdays = []int{2, 4, 6} }},
			{"no weekdays", maintenance.UpdateModel{Weekdays: []int{}}, func(m *maintenance.Model) { m.Weekdays = []int{} }},
			{"days of month", maintenance.UpdateModel{DaysOfMonth: []int{1, 15, 28}}, func(m *maintenance.Model) { m.DaysOfMonth = []int{1, 15, 28} }},
			{"no days of month", maintenance.UpdateModel{DaysOfMonth: []int{}}, func(m *maintenance.Model) { m.DaysOfMonth = []int{} }},
			{"interval day", maintenance.UpdateModel{IntervalDay: ptr(7)}, func(m *maintenance.Model) { m.IntervalDay = ptr(7) }},
			{"zero interval day", maintenance.UpdateModel{IntervalDay: ptr(0)}, func(m *maintenance.Model) { m.IntervalDay = ptr(0) }},
			{"cron", maintenance.UpdateModel{Cron: ptr("30 1 * * *")}, func(m *maintenance.Model) { m.Cron = ptr("30 1 * * *") }},
			{"empty cron", maintenance.UpdateModel{Cron: ptr("")}, func(m *maintenance.Model) { m.Cron = ptr("") }},
			{"timezone", maintenance.UpdateModel{Timezone: ptr("Europe/Berlin")}, func(m *maintenance.Model) { m.Timezone = ptr("Europe/Berlin") }},
			{"duration", maintenance.UpdateModel{Duration: ptr(45)}, func(m *maintenance.Model) { m.Duration = ptr(45) }},
			{"zero duration", maintenance.UpdateModel{Duration: ptr(0)}, func(m *maintenance.Model) { m.Duration = ptr(0) }},
			{"every column", maintenance.UpdateModel{
				Title: ptr("T"), Description: ptr("D"), Active: ptr(false), Strategy: ptr("single"),
				StartDateTime: ptr("2028-01-01T00:00"), EndDateTime: ptr("2028-01-02T00:00"), StartTime: ptr("10:00"), EndTime: ptr("11:00"),
				Weekdays: []int{7}, DaysOfMonth: []int{31}, IntervalDay: ptr(3), Cron: ptr("1 2 3 4 5"), Timezone: ptr("Asia/Tokyo"), Duration: ptr(5),
			}, func(m *maintenance.Model) {
				*m = maintenance.Model{
					Title: "T", Description: "D", Active: false, Strategy: "single",
					StartDateTime: ptr("2028-01-01T00:00"), EndDateTime: ptr("2028-01-02T00:00"), StartTime: ptr("10:00"), EndTime: ptr("11:00"),
					Weekdays: []int{7}, DaysOfMonth: []int{31}, IntervalDay: ptr(3), Cron: ptr("1 2 3 4 5"), Timezone: ptr("Asia/Tokyo"), Duration: ptr(5),
				}
			}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				created, err := repo.Create(ctx, workspaceID, baseWindow())
				require.NoError(t, err)
				time.Sleep(20 * time.Millisecond)

				patch := tt.patch
				updated, err := repo.UpdatePartial(ctx, workspaceID, created.ID, &patch)
				require.NoError(t, err)

				want := baseWindow()
				tt.change(want)
				want.ID, want.WorkspaceID, want.CreatedAt = created.ID, workspaceID, created.CreatedAt
				want.UpdatedAt = updated.UpdatedAt
				assert.Equal(t, want, updated)
				assert.True(t, updated.UpdatedAt.After(created.UpdatedAt), "a write moves updated_at")

				read, err := repo.FindByID(ctx, workspaceID, created.ID)
				require.NoError(t, err)
				assert.Equal(t, updated, read)
			})
		}
	})
}

// A partial update that names nothing writes nothing, and still says whether the window is there.
func TestSQLRepository_UpdatePartialWithNothingToChange(t *testing.T) {
	onEachDatabase(t, func(t *testing.T, repo maintenance.Repository, workspaceID, otherWorkspaceID workspace.ID) {
		ctx := context.Background()
		created, err := repo.Create(ctx, workspaceID, baseWindow())
		require.NoError(t, err)
		time.Sleep(20 * time.Millisecond)

		updated, err := repo.UpdatePartial(ctx, workspaceID, created.ID, &maintenance.UpdateModel{})
		require.NoError(t, err)
		assert.Equal(t, created.UpdatedAt, updated.UpdatedAt, "no write moves updated_at")
		assert.Equal(t, created, updated)

		_, err = repo.UpdatePartial(ctx, workspaceID, uuid.NewString(), &maintenance.UpdateModel{})
		assert.ErrorIs(t, err, maintenance.ErrNotFound)
		_, err = repo.UpdatePartial(ctx, otherWorkspaceID, created.ID, &maintenance.UpdateModel{})
		assert.ErrorIs(t, err, maintenance.ErrNotFound)
	})
}

// A partial update of a window that isn't in the workspace is not found, and changes nothing.
func TestSQLRepository_UpdatePartialOfAWindowThatIsNotThere(t *testing.T) {
	onEachDatabase(t, func(t *testing.T, repo maintenance.Repository, workspaceID, otherWorkspaceID workspace.ID) {
		ctx := context.Background()
		created, err := repo.Create(ctx, workspaceID, baseWindow())
		require.NoError(t, err)

		_, err = repo.UpdatePartial(ctx, workspaceID, uuid.NewString(), &maintenance.UpdateModel{Title: ptr("Nobody")})
		assert.ErrorIs(t, err, maintenance.ErrNotFound)
		got, err := repo.UpdatePartial(ctx, otherWorkspaceID, created.ID, &maintenance.UpdateModel{Title: ptr("Hijacked"), Active: ptr(false)})
		assert.ErrorIs(t, err, maintenance.ErrNotFound)
		assert.Nil(t, got)

		read, err := repo.FindByID(ctx, workspaceID, created.ID)
		require.NoError(t, err)
		assert.Equal(t, created, read)
	})
}
