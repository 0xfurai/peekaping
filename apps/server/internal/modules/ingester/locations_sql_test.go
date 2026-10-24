package ingester

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// The ingester over the real repositories: a monitor on two probes, in a migrated SQLite database
func TestService_IngestJudgesEachProbeOfAMonitorOverSQLite(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop().Sugar()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "ingester.db")}, logger)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err = migrator.Migrate(ctx)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO probes (id, name, status) VALUES (?, ?, 'online')", probeA, "Frankfurt")
	require.NoError(t, err)
	monitors := monitor.NewSQLRepository(db)
	created, err := monitors.Create(ctx, &shared.Monitor{Type: "http", Name: "Checkout", Interval: 60, Timeout: 16, Active: true, ProbeIDs: []string{"local", probeA}})
	require.NoError(t, err)
	bus := eventstest.NewBus()
	heartbeats := heartbeat.NewService(heartbeat.NewSQLRepository(db), bus, logger)
	service := NewService(heartbeats, &fakeCertificateService{}, monitors, bus, logger)
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	ingest := func(probeID string, status shared.MonitorStatus, minute int) {
		t.Helper()
		start := at.Add(time.Duration(minute) * time.Minute)
		require.NoError(t, service.Ingest(ctx, &Result{
			ProbeID: probeID, MonitorID: created.ID, MonitorName: "Checkout", MonitorType: "http", MonitorMaxRetries: 1,
			Status: status, PingMs: 100 + minute, StartTime: start, EndTime: start,
		}))
	}

	ingest("local", up, 0)
	ingest(probeA, up, 1)
	ingest("local", down, 2) // Pending: local has a retry left
	ingest(probeA, down, 3)  // Pending too: its own count, not local's
	ingest("local", down, 4) // Down
	ingest(probeA, up, 5)    // A is Up, and local is still Down

	beats, err := heartbeats.FindByMonitorIDAndProbeIDPaginated(ctx, created.ID, "local", 10, 0, nil, true)
	require.NoError(t, err)
	var local []string
	for _, hb := range beats {
		local = append(local, map[shared.MonitorStatus]string{up: "up", down: "down", pending: "pending"}[hb.Status])
	}
	assert.Equal(t, []string{"up", "pending", "down"}, local, "local is judged on its own heartbeats")
	beats, err = heartbeats.FindByMonitorIDAndProbeIDPaginated(ctx, created.ID, probeA, 10, 0, nil, true)
	require.NoError(t, err)
	var frankfurt []string
	for _, hb := range beats {
		frankfurt = append(frankfurt, map[shared.MonitorStatus]string{up: "up", down: "down", pending: "pending"}[hb.Status])
	}
	assert.Equal(t, []string{"up", "pending", "up"}, frankfurt, "and so is the remote probe")

	found, err := monitors.FindByID(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, found.Locations, 2)
	byProbe := map[string]shared.MonitorLocation{found.Locations[0].ProbeID: found.Locations[0], found.Locations[1].ProbeID: found.Locations[1]}
	require.NotNil(t, byProbe["local"].Status)
	assert.Equal(t, down, *byProbe["local"].Status)
	require.NotNil(t, byProbe["local"].LastPing)
	assert.Equal(t, 104, *byProbe["local"].LastPing)
	require.NotNil(t, byProbe[probeA].Status)
	assert.Equal(t, up, *byProbe[probeA].Status)
	require.NotNil(t, byProbe[probeA].LastHeartbeatAt)
	assert.True(t, at.Add(5*time.Minute).Equal(*byProbe[probeA].LastHeartbeatAt), "got %s", byProbe[probeA].LastHeartbeatAt)
	statusChanges := eventstest.Published(bus, events.MonitorStatusChanged)
	require.NotEmpty(t, statusChanges)
	assert.Equal(t, down, statusChanges[len(statusChanges)-1].Status, "the monitor went Down when local did, and stays Down while it is")
}

// The same over the real repositories: the monitor's recorded status survives a probe going offline,
// and a reset makes the next result the monitor's first
func TestService_IngestAnnouncesAChangeThatAnOfflineProbeWouldHideOverSQLite(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop().Sugar()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "ingester.db")}, logger)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err = migrator.Migrate(ctx)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO probes (id, name, status) VALUES (?, ?, 'online')", probeA, "Frankfurt")
	require.NoError(t, err)
	monitors := monitor.NewSQLRepository(db)
	created, err := monitors.Create(ctx, &shared.Monitor{Type: "http", Name: "Checkout", Interval: 60, Timeout: 16, Active: true, ProbeIDs: []string{"local", probeA}})
	require.NoError(t, err)
	bus := eventstest.NewBus()
	heartbeats := heartbeat.NewService(heartbeat.NewSQLRepository(db), bus, logger)
	service := NewService(heartbeats, &fakeCertificateService{}, monitors, bus, logger)
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	minute := 0
	ingest := func(probeID string, status shared.MonitorStatus) []string {
		t.Helper()
		minute++
		start := at.Add(time.Duration(minute) * time.Minute)
		require.NoError(t, service.Ingest(ctx, &Result{ProbeID: probeID, MonitorID: created.ID, MonitorName: "Checkout", MonitorType: "http", Status: status, StartTime: start, EndTime: start}))
		var topics []string
		for _, topic := range bus.TakeTopics() {
			if topic != events.HeartbeatEvent.Name() {
				topics = append(topics, topic)
			}
		}
		return topics
	}

	assert.Equal(t, []string{changed, notified}, ingest("local", up), "the first result")
	assert.Equal(t, []string{changed, notified}, ingest(probeA, down), "the monitor goes Down")
	_, err = db.Exec("UPDATE probes SET status = 'offline' WHERE id = ?", probeA)
	require.NoError(t, err)

	assert.Equal(t, []string{changed, notified}, ingest("local", up), "A is offline, so the monitor is Up: Down to Up, announced")
	assert.Empty(t, ingest("local", up), "and only once")

	// A reset, as the monitor service does it
	require.NoError(t, heartbeats.DeleteByMonitorID(ctx, created.ID))
	require.NoError(t, monitors.ResetLocations(ctx, created.ID))
	assert.Equal(t, []string{changed, notified}, ingest("local", up), "after a reset the next result is the monitor's first")
}
