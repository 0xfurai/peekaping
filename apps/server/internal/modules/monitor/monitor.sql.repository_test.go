package monitor

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/infra"
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
	"go.uber.org/zap"
)

func setupTestDB(t *testing.T) *bun.DB {
	sqldb, err := sql.Open(sqliteshim.ShimName, "file::memory:?cache=shared")
	require.NoError(t, err)

	db := bun.NewDB(sqldb, sqlitedialect.New())

	// Create monitors table
	_, err = db.Exec(`
		CREATE TABLE monitors (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			name TEXT NOT NULL,
			interval INTEGER NOT NULL,
			timeout INTEGER NOT NULL,
			max_retries INTEGER NOT NULL,
			retry_interval INTEGER NOT NULL,
			resend_interval INTEGER NOT NULL,
			active BOOLEAN NOT NULL DEFAULT TRUE,
			status INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			config TEXT,
			proxy_id TEXT,
			push_token TEXT,
			locations_status INTEGER,
			location_rule TEXT NOT NULL DEFAULT 'any',
			location_threshold INTEGER
		)
	`)
	require.NoError(t, err)

	// Create monitor_tags table for testing JOIN functionality
	_, err = db.Exec(`
		CREATE TABLE monitor_tags (
			monitor_id TEXT NOT NULL,
			tag_id TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (monitor_id, tag_id)
		)
	`)
	require.NoError(t, err)

	// Create monitor_probes table, where a monitor's probes are kept
	_, err = db.Exec(`
		CREATE TABLE monitor_probes (
			monitor_id TEXT NOT NULL,
			probe_id TEXT NOT NULL,
			status INTEGER,
			last_ping INTEGER,
			last_heartbeat_at DATETIME,
			PRIMARY KEY (monitor_id, probe_id)
		)
	`)
	require.NoError(t, err)

	// and the probes table they point at, which the monitors' locations are read with
	_, err = db.Exec(`
		CREATE TABLE probes (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'offline'
		)
	`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO probes (id, name) VALUES ('local', 'Local')`)
	require.NoError(t, err)

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

func createTestMonitor(name string, active bool, status shared.MonitorStatus) *shared.Monitor {
	return &shared.Monitor{
		Type:           "http",
		Name:           name,
		Interval:       60,
		Timeout:        30,
		MaxRetries:     3,
		RetryInterval:  60,
		ResendInterval: 5,
		Active:         active,
		Status:         status,
		Config:         `{"url": "https://example.com"}`,
		ProxyId:        "",
		PushToken:      "test-token",
	}
}

func TestSQLRepositoryImpl_FindAll_JoinAmbiguityFix(t *testing.T) {
	db := setupTestDB(t)
	repo := NewSQLRepository(db)
	ctx := context.Background()

	// Create a monitor
	monitor := createTestMonitor("Test Monitor", true, shared.MonitorStatus(0))
	created, err := repo.Create(ctx, monitor)
	require.NoError(t, err)

	// Add a tag to trigger the JOIN
	_, err = db.Exec("INSERT INTO monitor_tags (monitor_id, tag_id, created_at) VALUES (?, ?, ?)",
		created.ID, "test-tag", time.Now().Add(-1*time.Hour))
	require.NoError(t, err)

	t.Run("FindAll_WithTagsNoAmbiguousColumn", func(t *testing.T) {
		// This should not fail with "ambiguous column name: created_at" error
		monitors, err := repo.FindAll(ctx, 0, 10, "", nil, nil, []string{"test-tag"})

		require.NoError(t, err)
		assert.Len(t, monitors, 1)
		assert.Equal(t, created.ID, monitors[0].ID)

		// Verify the monitor's created_at is from the monitors table, not monitor_tags
		assert.WithinDuration(t, created.CreatedAt, monitors[0].CreatedAt, time.Second)
	})

	t.Run("FindAll_OrderingStillWorksWithJoin", func(t *testing.T) {
		// Create another monitor with tag to test ordering with JOIN
		monitor2 := createTestMonitor("Test Monitor 2", true, shared.MonitorStatus(0))
		created2, err := repo.Create(ctx, monitor2)
		require.NoError(t, err)

		_, err = db.Exec("INSERT INTO monitor_tags (monitor_id, tag_id, created_at) VALUES (?, ?, ?)",
			created2.ID, "test-tag", time.Now().Add(-2*time.Hour))
		require.NoError(t, err)

		monitors, err := repo.FindAll(ctx, 0, 10, "", nil, nil, []string{"test-tag"})

		require.NoError(t, err)
		assert.Len(t, monitors, 2)

		// Should be ordered by monitors.created_at DESC (newest first)
		assert.Equal(t, created2.ID, monitors[0].ID)
		assert.Equal(t, created.ID, monitors[1].ID)
	})
}

// Offset paging only visits every monitor once if the order never changes between pages, so
// monitors created at the same time have to come back in a fixed order. Create stamps the
// time itself, so the tie is inserted directly.
func TestSQLRepositoryImpl_FindAll_PagesMonitorsCreatedAtTheSameTimeByID(t *testing.T) {
	db := setupTestDB(t)
	repo := NewSQLRepository(db)
	ctx := context.Background()

	createdAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"monitor-1", "monitor-2", "monitor-3", "monitor-4", "monitor-5"} {
		_, err := db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval, created_at) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0, ?)`, id, createdAt)
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO monitor_tags (monitor_id, tag_id) VALUES (?, 'tie')", id)
		require.NoError(t, err)
	}

	for name, tagIds := range map[string][]string{"all monitors": nil, "filtered by tag": {"tie"}} {
		t.Run(name, func(t *testing.T) {
			var paged []string
			for page := 0; ; page++ {
				monitors, err := repo.FindAll(ctx, page, 2, "", nil, nil, tagIds)
				require.NoError(t, err)
				for _, m := range monitors {
					paged = append(paged, m.ID)
				}
				if len(monitors) < 2 {
					break
				}
			}

			assert.Equal(t, []string{"monitor-5", "monitor-4", "monitor-3", "monitor-2", "monitor-1"}, paged)
		})
	}
}

// sqliteDB opens a SQLite database the way the app does, with foreign keys on.
func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "monitor.db")}, zap.NewNop().Sugar())
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
	schema := "monitor_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

func migrateUp(t *testing.T, db *bun.DB, ms *migrate.Migrations) {
	t.Helper()
	ctx := context.Background()
	migrator := migrate.NewMigrator(db, ms)
	require.NoError(t, migrator.Init(ctx))
	_, err := migrator.Migrate(ctx)
	require.NoError(t, err)
}

// forEachSQLBackend runs fn on a migrated database of every SQL storage backend. PostgreSQL runs
// when PEE_TEST_POSTGRES_DSN is set.
func forEachSQLBackend(t *testing.T, fn func(t *testing.T, db *bun.DB)) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			migrateUp(t, db, migrations.Migrations)
			fn(t, db)
		})
	}
}

// addProbe stores a remote probe, which a monitor can be assigned to
func addProbe(t *testing.T, db *bun.DB, id string) {
	t.Helper()
	_, err := db.Exec("INSERT INTO probes (id, name) VALUES (?, ?)", id, "Probe "+id)
	require.NoError(t, err)
}

// monitorOn is a monitor that runs on the given probes
func monitorOn(name string, active bool, probeIDs ...string) *shared.Monitor {
	monitor := createTestMonitor(name, active, shared.MonitorStatusPending)
	monitor.ProbeIDs = probeIDs
	return monitor
}

func idsOf(monitors []*Model) []string {
	ids := make([]string, len(monitors))
	for i, m := range monitors {
		ids[i] = m.ID
	}
	return ids
}

// addMonitorProbesMigration is the migration that gave monitors their probes
const addMonitorProbesMigration = "20261023120000"

func TestSQLRepository_MonitorsAreBackfilledToLocal(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := open(t)
			before := migrate.NewMigrations()
			for _, m := range migrations.Migrations.Sorted() {
				if m.Name < addMonitorProbesMigration {
					before.Add(m)
				}
			}
			migrateUp(t, db, before)
			var existing []string
			for range 3 {
				id := uuid.NewString()
				existing = append(existing, id)
				_, err := db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, id)
				require.NoError(t, err)
			}

			migrateUp(t, db, migrations.Migrations)

			// The rows themselves: a monitor without one would read as local anyway
			for _, id := range existing {
				var probeIDs []string
				require.NoError(t, db.NewSelect().Table("monitor_probes").Column("probe_id").Where("monitor_id = ?", id).Scan(ctx, &probeIDs))
				assert.Equal(t, []string{"local"}, probeIDs, id)
			}
			found, err := NewSQLRepository(db).FindByIDs(ctx, existing)
			require.NoError(t, err)
			require.Len(t, found, 3)
			for _, m := range found {
				assert.Equal(t, []string{"local"}, m.ProbeIDs, m.ID)
			}
		})
	}
}

func TestSQLRepository_CreateStoresProbeIDs(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		addProbe(t, db, "frankfurt")

		onProbe, err := repo.Create(ctx, monitorOn("On a probe", true, "frankfurt"))
		require.NoError(t, err)
		unassigned, err := repo.Create(ctx, monitorOn("Unassigned", true))
		require.NoError(t, err)

		assert.Equal(t, []string{"frankfurt"}, onProbe.ProbeIDs)
		assert.Equal(t, []string{"local"}, unassigned.ProbeIDs, "a monitor with no probe runs on the local one")
		for id, want := range map[string][]string{onProbe.ID: {"frankfurt"}, unassigned.ID: {"local"}} {
			found, err := repo.FindByID(ctx, id)
			require.NoError(t, err)
			assert.Equal(t, want, found.ProbeIDs)
		}
		all, err := repo.FindAll(ctx, 0, 10, "", nil, nil, nil)
		require.NoError(t, err)
		require.Len(t, all, 2)
		for _, m := range all {
			assert.NotEmpty(t, m.ProbeIDs, "a list carries the probes too: %s", m.Name)
		}
	})
}

func TestSQLRepository_UpdatesReplaceTheProbesOnlyWhenGiven(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		addProbe(t, db, "frankfurt")
		addProbe(t, db, "paris")
		created, err := repo.Create(ctx, monitorOn("Checkout", true, "frankfurt"))
		require.NoError(t, err)
		probesOf := func() []string {
			found, err := repo.FindByID(ctx, created.ID)
			require.NoError(t, err)
			return found.ProbeIDs
		}

		renamed := *created
		renamed.Name, renamed.ProbeIDs = "Checkout, renamed", nil
		require.NoError(t, repo.UpdateFull(ctx, created.ID, &renamed))
		assert.Equal(t, []string{"frankfurt"}, probesOf(), "a full update without probes keeps them")

		renamed.ProbeIDs = []string{"paris"}
		require.NoError(t, repo.UpdateFull(ctx, created.ID, &renamed))
		assert.Equal(t, []string{"paris"}, probesOf(), "and replaces them with the given ones")

		name := "Checkout, again"
		require.NoError(t, repo.UpdatePartial(ctx, created.ID, &UpdateModel{Name: &name}))
		assert.Equal(t, []string{"paris"}, probesOf(), "a partial update without probes keeps them")

		require.NoError(t, repo.UpdatePartial(ctx, created.ID, &UpdateModel{ProbeIDs: []string{"local"}}))
		assert.Equal(t, []string{"local"}, probesOf(), "and moves the monitor when the probes are all it changes")
	})
}

func TestSQLRepository_FindActiveByProbe(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		addProbe(t, db, "frankfurt")
		addProbe(t, db, "paris")
		var local, frankfurt []string
		for i, spec := range []struct {
			probe  string
			active bool
		}{{"local", true}, {"frankfurt", true}, {"local", true}, {"frankfurt", false}, {"paris", true}, {"frankfurt", true}, {"local", false}} {
			created, err := repo.Create(ctx, monitorOn("Monitor "+string(rune('a'+i)), spec.active, spec.probe))
			require.NoError(t, err)
			switch {
			case spec.probe == "local" && spec.active:
				local = append(local, created.ID)
			case spec.probe == "frankfurt" && spec.active:
				frankfurt = append(frankfurt, created.ID)
			}
		}

		got, err := repo.FindActiveByProbe(ctx, "frankfurt", 0, 10)
		require.NoError(t, err)
		assert.ElementsMatch(t, frankfurt, idsOf(got), "the active monitors of the probe, and no others")
		for _, m := range got {
			assert.Equal(t, []string{"frankfurt"}, m.ProbeIDs)
		}
		got, err = repo.FindActiveByProbe(ctx, "local", 0, 10)
		require.NoError(t, err)
		assert.ElementsMatch(t, local, idsOf(got))
		got, err = repo.FindActiveByProbe(ctx, "a-probe-with-no-monitors", 0, 10)
		require.NoError(t, err)
		assert.Empty(t, got)

		var paged []string
		for page := 0; ; page++ {
			got, err := repo.FindActiveByProbe(ctx, "local", page, 1)
			require.NoError(t, err)
			paged = append(paged, idsOf(got)...)
			if len(got) < 1 {
				break
			}
		}
		assert.ElementsMatch(t, local, paged, "pages visit each monitor once")
		assert.Len(t, paged, 2)
	})
}

func TestSQLRepository_DeletingAMonitorRemovesItsProbeRows(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		addProbe(t, db, "frankfurt")
		created, err := repo.Create(ctx, monitorOn("Checkout", true, "frankfurt"))
		require.NoError(t, err)

		require.NoError(t, repo.Delete(ctx, created.ID))

		var rows int
		require.NoError(t, db.NewSelect().Table("monitor_probes").ColumnExpr("count(*)").Where("monitor_id = ?", created.ID).Scan(ctx, &rows))
		assert.Zero(t, rows)
		_, err = db.Exec("DELETE FROM probes WHERE id = ?", "frankfurt")
		assert.NoError(t, err, "and the probe is free to go again")
	})
}

// A query whose context is cancelled costs SQLite its connection, and the next query runs on a new
// one. Deleting a monitor then must still cascade to its rows: that needs foreign keys on.
func TestSQLRepository_DeletingAMonitorAfterACancelledQueryStillRemovesItsRows(t *testing.T) {
	t.Setenv("BUNDEBUG", "0") // the query hook prints a cancelled query as a failure
	db := sqliteDB(t)
	migrateUp(t, db, migrations.Migrations)
	repo := NewSQLRepository(db)
	ctx := context.Background()
	addProbe(t, db, "frankfurt")
	created, err := repo.Create(ctx, monitorOn("Checkout", true, "frankfurt"))
	require.NoError(t, err)
	for range 3 {
		_, err = db.Exec("INSERT INTO heartbeats (id, monitor_id, status) VALUES (?, ?, 1)", uuid.NewString(), created.ID)
		require.NoError(t, err)
	}

	// A TEMP table lives as long as its connection, so it is the marker: when it is gone, the delete
	// runs on a new connection, and the test can't pass when the cancel never reached the driver.
	replaced := false
	for attempt := 0; attempt < 20 && !replaced; attempt++ {
		_, err = db.Exec("CREATE TEMP TABLE IF NOT EXISTS connection_marker (x INTEGER)")
		require.NoError(t, err)
		slowCtx, cancel := context.WithCancel(ctx)
		timer := time.AfterFunc(50*time.Millisecond, cancel)
		var n int64
		err := db.NewRaw("WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c WHERE x < 2000000000) SELECT count(*) FROM c").Scan(slowCtx, &n)
		timer.Stop()
		cancel()
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
		var markers int
		require.NoError(t, db.NewRaw("SELECT count(*) FROM sqlite_temp_master WHERE name = 'connection_marker'").Scan(ctx, &markers))
		replaced = markers == 0
	}
	require.True(t, replaced, "the cancelled query never cost the connection")

	require.NoError(t, repo.Delete(ctx, created.ID))

	for _, table := range []string{"monitor_probes", "heartbeats"} {
		var rows int
		require.NoError(t, db.NewSelect().Table(table).ColumnExpr("count(*)").Where("monitor_id = ?", created.ID).Scan(ctx, &rows))
		assert.Zero(t, rows, table)
	}
}

func TestSQLRepository_TheDatabaseRefusesToDeleteAProbeWithMonitors(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		addProbe(t, db, "frankfurt")
		created, err := repo.Create(ctx, monitorOn("Checkout", true, "frankfurt"))
		require.NoError(t, err)

		_, err = db.Exec("DELETE FROM probes WHERE id = ?", "frankfurt")

		assert.Error(t, err)
		found, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, []string{"frankfurt"}, found.ProbeIDs, "the monitor keeps its probe")
	})
}

// A monitor and its probe rows are written in the transaction the service opens: when it rolls
// back, neither stays.
func TestSQLRepository_ProbeRowsFollowTheMonitorsTransaction(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		addProbe(t, db, "frankfurt")

		err := dbtx.NewSQLTransactor(db).WithTx(ctx, func(ctx context.Context) error {
			_, err := repo.Create(ctx, monitorOn("Checkout", true, "frankfurt"))
			require.NoError(t, err)
			return assert.AnError
		})

		require.ErrorIs(t, err, assert.AnError)
		var rows int
		require.NoError(t, db.NewSelect().Table("monitor_probes").ColumnExpr("count(*)").Where("probe_id = ?", "frankfurt").Scan(ctx, &rows))
		assert.Zero(t, rows)
	})
}

// A monitor that a process from before probes created while an upgrade rolled out, or that was
// copied from MongoDB, has no row. It runs on the local probe, as its model says.
func TestSQLRepository_AMonitorWithoutARowRunsOnLocal(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		addProbe(t, db, "frankfurt")
		rowless := uuid.NewString()
		_, err := db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval, active) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0, ?)`, rowless, true)
		require.NoError(t, err)
		remote, err := repo.Create(ctx, monitorOn("On a probe", true, "frankfurt"))
		require.NoError(t, err)

		found, err := repo.FindByID(ctx, rowless)
		require.NoError(t, err)
		assert.Equal(t, []string{"local"}, found.ProbeIDs)
		onLocal, err := repo.FindActiveByProbe(ctx, "local", 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{rowless}, idsOf(onLocal))
		onRemote, err := repo.FindActiveByProbe(ctx, "frankfurt", 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{remote.ID}, idsOf(onRemote))
	})
}
