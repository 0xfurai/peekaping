package monitor

import (
	"context"
	"sync"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/shared"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

const (
	down        = shared.MonitorStatusDown
	up          = shared.MonitorStatusUp
	pending     = shared.MonitorStatusPending
	maintenance = shared.MonitorStatusMaintenance
)

// stateOf is the state a result leaves on a probe's row
func stateOf(probeID string, status shared.MonitorStatus, ping int, at time.Time) shared.LocationState {
	return shared.LocationState{ProbeID: probeID, Status: &status, LastPing: &ping, LastHeartbeatAt: &at}
}

func statusOf(l shared.MonitorLocation) string {
	if l.Status == nil {
		return "unknown"
	}
	return map[shared.MonitorStatus]string{down: "down", up: "up", pending: "pending", maintenance: "maintenance"}[*l.Status]
}

func statuses(locations []shared.MonitorLocation) map[string]string {
	got := map[string]string{}
	for _, l := range locations {
		got[l.ProbeID] = statusOf(l)
	}
	return got
}

func setProbeStatus(t *testing.T, db *bun.DB, id, status string) {
	t.Helper()
	_, err := db.Exec("UPDATE probes SET status = ? WHERE id = ?", status, id)
	require.NoError(t, err)
}

func TestSQLRepository_ALocationStartsWithNoStatus(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		addProbe(t, db, "frankfurt")
		setProbeStatus(t, db, "frankfurt", "online")
		addProbe(t, db, "paris")

		created, err := repo.Create(ctx, monitorOn("Checkout", true, "paris", "local", "frankfurt"))
		require.NoError(t, err)

		want := []shared.MonitorLocation{
			{ProbeID: "frankfurt", ProbeName: "Probe frankfurt", Online: true},
			{ProbeID: "local", ProbeName: "Local", Online: true},
			{ProbeID: "paris", ProbeName: "Probe paris", Online: false},
		}
		assert.Equal(t, want, created.Locations, "the create's answer")
		found, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, want, found.Locations, "one monitor")
		assert.Equal(t, []string{"frankfurt", "local", "paris"}, found.ProbeIDs, "in the order of its probe_ids")
		all, err := repo.FindAll(ctx, 0, 10, "", nil, nil, nil)
		require.NoError(t, err)
		require.Len(t, all, 1)
		assert.Equal(t, want, all[0].Locations, "a list")
	})
}

func TestSQLRepository_TheLocalProbeIsAlwaysOnline(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		setProbeStatus(t, db, "local", "offline") // nothing speaks the probe protocol on most installs
		created, err := repo.Create(ctx, monitorOn("Checkout", true, "local"))
		require.NoError(t, err)

		assert.True(t, created.Locations[0].Online)
	})
}

func TestSQLRepository_RecordLocationKeepsEachProbesState(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		addProbe(t, db, "frankfurt")
		created, err := repo.Create(ctx, monitorOn("Checkout", true, "local", "frankfurt"))
		require.NoError(t, err)
		other, err := repo.Create(ctx, monitorOn("Search", true, "local", "frankfurt"))
		require.NoError(t, err)
		at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

		before, after, err := repo.RecordLocation(ctx, created.ID, stateOf("frankfurt", down, 450, at))
		require.NoError(t, err)

		assert.Equal(t, map[string]string{"frankfurt": "unknown", "local": "unknown"}, statuses(before))
		assert.Equal(t, map[string]string{"frankfurt": "down", "local": "unknown"}, statuses(after))
		require.NotNil(t, after[0].LastPing)
		assert.Equal(t, 450, *after[0].LastPing)
		require.NotNil(t, after[0].LastHeartbeatAt)
		assert.True(t, at.Equal(*after[0].LastHeartbeatAt), "got %s", after[0].LastHeartbeatAt)

		before, after, err = repo.RecordLocation(ctx, created.ID, stateOf("local", up, 20, at.Add(time.Minute)))
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"frankfurt": "down", "local": "unknown"}, statuses(before), "the next result sees the last one")
		assert.Equal(t, map[string]string{"frankfurt": "down", "local": "up"}, statuses(after), "and the other probe keeps its state")

		found, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, after, found.Locations, "the monitor reads what was recorded")
		untouched, err := repo.FindByID(ctx, other.ID)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"frankfurt": "unknown", "local": "unknown"}, statuses(untouched.Locations), "another monitor's rows are left alone")
	})
}

func TestSQLRepository_RecordLocationForAProbeTheMonitorDoesNotRunOn(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		addProbe(t, db, "frankfurt")
		created, err := repo.Create(ctx, monitorOn("Checkout", true, "local"))
		require.NoError(t, err)

		before, after, err := repo.RecordLocation(ctx, created.ID, stateOf("frankfurt", up, 20, time.Now()))

		require.NoError(t, err)
		assert.Equal(t, map[string]string{"local": "unknown"}, statuses(before))
		assert.Equal(t, map[string]string{"local": "unknown"}, statuses(after), "a probe that was taken off the monitor is not put back")
		found, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, []string{"local"}, found.ProbeIDs)
	})
}

func TestSQLRepository_RecordLocationCanClearTheState(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		created, err := repo.Create(ctx, monitorOn("Checkout", true, "local"))
		require.NoError(t, err)
		_, _, err = repo.RecordLocation(ctx, created.ID, stateOf("local", up, 20, time.Now()))
		require.NoError(t, err)

		_, after, err := repo.RecordLocation(ctx, created.ID, shared.LocationState{ProbeID: "local"})

		require.NoError(t, err)
		assert.Equal(t, shared.MonitorLocation{ProbeID: "local", ProbeName: "Local", Online: true}, after[0], "a state of nils puts the row back as it was before the first result")
	})
}

func TestSQLRepository_ChangingProbesKeepsTheStateOfTheOnesThatStay(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		addProbe(t, db, "frankfurt")
		addProbe(t, db, "paris")
		created, err := repo.Create(ctx, monitorOn("Checkout", true, "local", "frankfurt"))
		require.NoError(t, err)
		at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
		_, _, err = repo.RecordLocation(ctx, created.ID, stateOf("local", up, 20, at))
		require.NoError(t, err)
		_, _, err = repo.RecordLocation(ctx, created.ID, stateOf("frankfurt", down, 450, at))
		require.NoError(t, err)

		renamed := *created
		renamed.Name, renamed.ProbeIDs = "Checkout, renamed", []string{"frankfurt", "paris"}
		require.NoError(t, repo.UpdateFull(ctx, created.ID, &renamed))
		found, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"frankfurt": "down", "paris": "unknown"}, statuses(found.Locations), "frankfurt stays, local is gone and paris is new")

		require.NoError(t, repo.UpdatePartial(ctx, created.ID, &UpdateModel{ProbeIDs: []string{"local", "frankfurt", "paris"}}))
		found, err = repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"frankfurt": "down", "local": "unknown", "paris": "unknown"}, statuses(found.Locations), "a probe that comes back starts over")
	})
}

func TestSQLRepository_AMonitorWithoutARowHasALocalLocation(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		rowless := uuid.NewString()
		_, err := db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval, active) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0, ?)`, rowless, true)
		require.NoError(t, err)

		found, err := repo.FindByID(ctx, rowless)
		require.NoError(t, err)
		assert.Equal(t, []shared.MonitorLocation{{ProbeID: "local", ProbeName: "Local", Online: true}}, found.Locations)

		before, after, err := repo.RecordLocation(ctx, rowless, stateOf("local", up, 20, time.Now()))
		require.NoError(t, err)
		assert.Empty(t, before, "there is no row to hold the state")
		assert.Empty(t, after)
	})
}

// Two results of one monitor that are recorded at once see each other in turn: one sees the
// monitor as the other left it. Without that, two probes that both fail at the same moment would
// each take the other's row for the old state, and nobody would see the monitor go down.
func TestSQLRepository_RecordLocationSerializesResultsOfOneMonitor(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		addProbe(t, db, "frankfurt")
		for round := range 20 {
			created, err := repo.Create(ctx, monitorOn("Checkout", true, "local", "frankfurt"))
			require.NoError(t, err)
			_, _, err = repo.RecordLocation(ctx, created.ID, stateOf("local", up, 20, time.Now()))
			require.NoError(t, err)
			_, _, err = repo.RecordLocation(ctx, created.ID, stateOf("frankfurt", up, 20, time.Now()))
			require.NoError(t, err)

			var wg sync.WaitGroup
			var seen [2]struct{ before, after map[string]string }
			for i, probeID := range []string{"local", "frankfurt"} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					before, after, err := repo.RecordLocation(ctx, created.ID, stateOf(probeID, down, 20, time.Now()))
					assert.NoError(t, err)
					seen[i].before, seen[i].after = statuses(before), statuses(after)
				}()
			}
			wg.Wait()

			bothDown := map[string]string{"local": "down", "frankfurt": "down"}
			bothUp := map[string]string{"local": "up", "frankfurt": "up"}
			first, second := seen[0], seen[1]
			if !assert.ObjectsAreEqual(bothUp, first.before) {
				first, second = second, first
			}
			assert.Equal(t, bothUp, first.before, "round %d: one result is first and sees the old state", round)
			assert.Equal(t, second.before, first.after, "round %d: and the other sees what the first left", round)
			assert.Equal(t, bothDown, second.after, "round %d", round)
		}
	})
}

// addMonitorProbeStateMigration is the migration that gave a monitor's probe rows their state
const addMonitorProbeStateMigration = "20261024120300"

func TestSQLRepository_ProbeStateIsBackfilledFromTheLatestHeartbeat(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := open(t)
			before := migrate.NewMigrations()
			for _, m := range migrations.Migrations.Sorted() {
				if m.Name < addMonitorProbeStateMigration {
					before.Add(m)
				}
			}
			migrateUp(t, db, before)
			addProbe(t, db, "frankfurt")
			beat := func(monitorID, probeID string, status shared.MonitorStatus, ping int, at time.Time) {
				_, err := db.Exec(`INSERT INTO heartbeats (id, monitor_id, probe_id, status, ping, time) VALUES (?, ?, ?, ?, ?, ?)`, uuid.NewString(), monitorID, probeID, int(status), ping, at)
				require.NoError(t, err)
			}
			newMonitor := func(probeIDs ...string) string {
				id := uuid.NewString()
				_, err := db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, id)
				require.NoError(t, err)
				for _, probeID := range probeIDs {
					_, err = db.Exec(`INSERT INTO monitor_probes (monitor_id, probe_id) VALUES (?, ?)`, id, probeID)
					require.NoError(t, err)
				}
				return id
			}
			noon := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
			checked := newMonitor("local")
			beat(checked, "local", up, 30, noon)
			beat(checked, "local", down, 900, noon.Add(time.Minute))
			beat(checked, "local", pending, 40, noon.Add(-time.Minute))
			onProbe := newMonitor("frankfurt")
			beat(onProbe, "frankfurt", up, 80, noon)
			neverChecked := newMonitor("local")

			migrateUp(t, db, migrations.Migrations)

			repo := NewSQLRepository(db)
			found, err := repo.FindByID(ctx, checked)
			require.NoError(t, err)
			require.Len(t, found.Locations, 1)
			assert.Equal(t, "down", statusOf(found.Locations[0]), "the latest heartbeat's status")
			require.NotNil(t, found.Locations[0].LastPing)
			assert.Equal(t, 900, *found.Locations[0].LastPing)
			require.NotNil(t, found.Locations[0].LastHeartbeatAt)
			assert.True(t, noon.Add(time.Minute).Equal(*found.Locations[0].LastHeartbeatAt), "got %s", found.Locations[0].LastHeartbeatAt)
			found, err = repo.FindByID(ctx, onProbe)
			require.NoError(t, err)
			assert.Equal(t, "up", statusOf(found.Locations[0]), "a remote probe's row too")
			found, err = repo.FindByID(ctx, neverChecked)
			require.NoError(t, err)
			assert.Equal(t, shared.MonitorLocation{ProbeID: "local", ProbeName: "Local", Online: true}, found.Locations[0], "a probe with no heartbeat has no state")
		})
	}
}
