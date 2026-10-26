package modules_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

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

// onEveryMonitorBackend runs exercise against a monitor repository on SQLite, and on PostgreSQL when
// its test variable is set.
func onEveryMonitorBackend(t *testing.T, exercise func(t *testing.T, repo monitor.MonitorRepository)) {
	t.Helper()
	onEveryMonitorAndHeartbeatBackend(t, func(t *testing.T, repo monitor.MonitorRepository, _ heartbeat.Repository) {
		exercise(t, repo)
	})
}

// onEveryMonitorAndHeartbeatBackend is onEveryMonitorBackend with the heartbeat repository of the same database.
func onEveryMonitorAndHeartbeatBackend(t *testing.T, exercise func(t *testing.T, repo monitor.MonitorRepository, heartbeats heartbeat.Repository)) {
	t.Helper()
	onEveryMonitorStore(t, func(t *testing.T, repo monitor.MonitorRepository, heartbeats heartbeat.Repository, _ insertTiedMonitors) {
		exercise(t, repo, heartbeats)
	})
}

// insertTiedMonitors writes n monitors straight to the database, ids in ascending order, all created
// at createdAt and all carrying one tag, and returns their ids and the tag's id. The repositories
// stamp the creation time themselves, so only a direct insert makes monitors tie on it.
type insertTiedMonitors func(t *testing.T, n int, createdAt time.Time) (ids []string, tagID string)

// onEveryMonitorStore is onEveryMonitorAndHeartbeatBackend with a way to insert rows the repositories cannot make.
func onEveryMonitorStore(t *testing.T, exercise func(t *testing.T, repo monitor.MonitorRepository, heartbeats heartbeat.Repository, insertTied insertTiedMonitors)) {
	t.Helper()
	sqlStore := func(t *testing.T, db *bun.DB) (monitor.MonitorRepository, heartbeat.Repository, insertTiedMonitors) {
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		migrator := migrate.NewMigrator(db, migrations.Migrations)
		require.NoError(t, migrator.Init(context.Background()))
		_, err := migrator.Migrate(context.Background())
		require.NoError(t, err)
		insertTied := func(t *testing.T, n int, createdAt time.Time) ([]string, string) {
			t.Helper()
			tagID := uuid.NewString()
			_, err := db.Exec("INSERT INTO tags (id, name) VALUES (?, 'tie')", tagID)
			require.NoError(t, err)
			ids := make([]string, 0, n)
			for i := 1; i <= n; i++ {
				id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
				_, err := db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval, created_at) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0, ?)`, id, createdAt)
				require.NoError(t, err)
				_, err = db.Exec("INSERT INTO monitor_tags (id, monitor_id, tag_id) VALUES (?, ?, ?)", uuid.NewString(), id, tagID)
				require.NoError(t, err)
				ids = append(ids, id)
			}
			return ids, tagID
		}
		return monitor.NewSQLRepository(db), heartbeat.NewSQLRepository(db), insertTied
	}

	t.Run("SQLite", func(t *testing.T) {
		sqlDB, err := sql.Open(sqliteshim.ShimName, filepath.Join(t.TempDir(), "monitor-status.db"))
		require.NoError(t, err)
		monitors, heartbeats, insertTied := sqlStore(t, bun.NewDB(sqlDB, sqlitedialect.New()))
		exercise(t, monitors, heartbeats, insertTied)
	})
	t.Run("Postgres", func(t *testing.T) {
		dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("set PEE_TEST_POSTGRES_DSN to run PostgreSQL integration checks")
		}
		admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
		schema := "pp189_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		_, err := admin.Exec("CREATE SCHEMA ?", bun.Ident(schema))
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := admin.Exec("DROP SCHEMA ? CASCADE", bun.Ident(schema))
			require.NoError(t, err)
			require.NoError(t, admin.Close())
		})
		connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn), pgdriver.WithConnParams(map[string]any{"search_path": schema}))
		monitors, heartbeats, insertTied := sqlStore(t, bun.NewDB(sql.OpenDB(connector), pgdialect.New()))
		exercise(t, monitors, heartbeats, insertTied)
	})
}

// The monitor list filters on the stored status, which only Heartbeats set, so a full update of
// a monitor's settings has to leave it alone.
func TestMonitorFullUpdateKeepsStoredStatus(t *testing.T) {
	onEveryMonitorBackend(t, func(t *testing.T, repo monitor.MonitorRepository) {
		ctx := context.Background()
		created, err := repo.Create(ctx, &monitor.Model{Type: "http", Name: "Monitor", Interval: 60, Timeout: 10, Active: true, Config: "{}"})
		require.NoError(t, err)
		status := shared.MonitorStatusMaintenance
		require.NoError(t, repo.UpdatePartial(ctx, created.ID, &monitor.UpdateModel{Status: &status}))

		err = repo.UpdateFull(ctx, created.ID, &monitor.Model{Type: "http", Name: "Renamed", Interval: 60, Timeout: 10, Active: true, Config: "{}", Status: shared.MonitorStatusUp})
		require.NoError(t, err)

		stored, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, "Renamed", stored.Name)
		assert.Equal(t, shared.MonitorStatusMaintenance, stored.Status)
	})
}

// The monitors list and the status repair page through monitors with an offset, which only visits
// each one once if monitors created at the same time always come back in the same order.
func TestMonitorListPagesMonitorsCreatedAtTheSameTimeByID(t *testing.T) {
	onEveryMonitorStore(t, func(t *testing.T, repo monitor.MonitorRepository, _ heartbeat.Repository, insertTied insertTiedMonitors) {
		ctx := context.Background()
		ids, tagID := insertTied(t, 5, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

		for name, tagIDs := range map[string][]string{"all monitors": nil, "filtered by tag": {tagID}} {
			t.Run(name, func(t *testing.T) {
				var paged []string
				for page := 0; ; page++ {
					monitors, err := repo.FindAll(ctx, page, 2, "", nil, nil, tagIDs)
					require.NoError(t, err)
					for _, m := range monitors {
						paged = append(paged, m.ID)
					}
					if len(monitors) < 2 {
						break
					}
				}

				assert.Equal(t, []string{ids[4], ids[3], ids[2], ids[1], ids[0]}, paged)
			})
		}
	})
}

func TestMonitorCreateStoresTheGivenStatus(t *testing.T) {
	onEveryMonitorBackend(t, func(t *testing.T, repo monitor.MonitorRepository) {
		ctx := context.Background()

		created, err := repo.Create(ctx, &monitor.Model{Type: "http", Name: "Monitor", Interval: 60, Timeout: 10, Active: true, Config: "{}", Status: shared.MonitorStatusPending})
		require.NoError(t, err)

		assert.Equal(t, shared.MonitorStatusPending, created.Status)
		stored, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, shared.MonitorStatusPending, stored.Status)
	})
}
