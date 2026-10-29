package cleanup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/installsettings"
	"peekaping/internal/limits"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacetest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func sqliteInstall(t *testing.T) install {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "cleanup.db")}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return newInstallOn(t, db)
}

// postgresInstall is an install in an empty schema of the database at PEE_TEST_POSTGRES_DSN
func postgresInstall(t *testing.T) install {
	t.Helper()
	dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PEE_TEST_POSTGRES_DSN to run the PostgreSQL tests")
	}
	admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	schema := "cleanup_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	return newInstallOn(t, db)
}

// monitorIn adds a monitor to the workspace and returns its ID
func (in install) monitorIn(t *testing.T, id workspace.ID) string {
	t.Helper()
	monitorID := uuid.NewString()
	_, err := in.db.Exec(`INSERT INTO monitors (id, workspace_id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, ?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, monitorID, string(id))
	require.NoError(t, err)
	return monitorID
}

// heartbeatsOf records a heartbeat of the monitor for each age in days, with the age as its message
func (in install) heartbeatsOf(t *testing.T, monitorID string, ages ...int) {
	t.Helper()
	now := time.Now().UTC()
	for _, days := range ages {
		row := &heartbeatRow{ID: uuid.NewString(), MonitorID: monitorID, Status: 1, Msg: fmt.Sprintf("%dd", days), Time: now.AddDate(0, 0, -days)}
		_, err := in.db.NewInsert().Model(row).Exec(context.Background())
		require.NoError(t, err)
	}
}

func (in install) heartbeatsOfLeft(t *testing.T, monitorID string) []string {
	t.Helper()
	left, err := in.heartbeats.FindByMonitorIDPaginated(context.Background(), monitorID, 100, 0, nil, true)
	require.NoError(t, err)
	msgs := []string{}
	for _, hb := range left {
		msgs = append(msgs, hb.Msg)
	}
	return msgs
}

// AC4: each workspace keeps the history its own period asks for
func TestCleanupHeartbeatsKeepsEachWorkspacesOwnHistory(t *testing.T) {
	for name, open := range map[string]func(*testing.T) install{"SQLite": sqliteInstall, "Postgres": postgresInstall} {
		t.Run(name, func(t *testing.T) {
			in := open(t)
			oneDay := in.workspaceID(t)
			tenDays := workspace.ID(uuid.NewString())
			workspacetest.Add(t, in.db, tenDays, "Ten days")
			in.storePeriod(t, oneDay, "1")
			in.storePeriod(t, tenDays, "10")
			shortMonitor := in.monitorID
			longMonitor := in.monitorIn(t, tenDays)
			in.heartbeatsOf(t, shortMonitor, 20, 5, 0)
			in.heartbeatsOf(t, longMonitor, 20, 5, 0)

			cleanupHeartbeats(in.heartbeats, in.workspaces, in.workspaceSettings, limits.Snapshot{}, zap.NewNop().Sugar())

			assert.Equal(t, []string{"0d"}, in.heartbeatsOfLeft(t, shortMonitor), "the workspace with one day keeps today's")
			assert.Equal(t, []string{"5d", "0d"}, in.heartbeatsOfLeft(t, longMonitor), "the workspace with ten days keeps today's and the one from five days ago")
		})
	}
}

func TestCleanupHeartbeatsGoesOnWithTheOtherWorkspacesWhenOnePeriodIsUnusable(t *testing.T) {
	in := newInstall(t)
	broken := in.workspaceID(t)
	healthy := workspace.ID(uuid.NewString())
	workspacetest.Add(t, in.db, healthy, "Healthy")
	in.storePeriod(t, broken, "abc")
	in.storePeriod(t, healthy, "10")
	brokenMonitor := in.monitorID
	healthyMonitor := in.monitorIn(t, healthy)
	in.heartbeatsOf(t, brokenMonitor, 20, 0)
	in.heartbeatsOf(t, healthyMonitor, 20, 0)

	cleanupHeartbeats(in.heartbeats, in.workspaces, in.workspaceSettings, limits.Snapshot{}, zap.NewNop().Sugar())

	assert.Equal(t, []string{"20d", "0d"}, in.heartbeatsOfLeft(t, brokenMonitor))
	assert.Equal(t, []string{"0d"}, in.heartbeatsOfLeft(t, healthyMonitor))
}

// caps are the workspaces' limits as the install keeps them, with no defaults: each workspace has
// only the limits of its own it is given
func (in install) caps() *limits.Chain {
	return limits.NewChain(limits.NewSQLStore(in.db), nil)
}

// snapshotOf is the caps one run of the cleanup goes by, read as the run reads them
func snapshotOf(t *testing.T, caps limits.Source) limits.Snapshot {
	t.Helper()
	snapshot, err := caps.Snapshot(context.Background())
	require.NoError(t, err)
	return snapshot
}

func capRetention(t *testing.T, caps *limits.Chain, id workspace.ID, days int) {
	t.Helper()
	require.NoError(t, caps.SetOwn(context.Background(), id, &limits.Limits{RetentionDays: limits.Of(days)}))
}

// PP-161 AC2, AC4: each workspace keeps the shorter of its own period and its cap, and lowering a
// cap trims the history on the next run
func TestCleanupHeartbeatsKeepsTheShorterOfEachWorkspacesPeriodAndItsCap(t *testing.T) {
	for name, open := range map[string]func(*testing.T) install{"SQLite": sqliteInstall, "Postgres": postgresInstall} {
		t.Run(name, func(t *testing.T) {
			in := open(t)
			caps := in.caps()
			capped := in.workspaceID(t)
			uncapped := workspace.ID(uuid.NewString())
			workspacetest.Add(t, in.db, uncapped, "Uncapped")
			in.storePeriod(t, capped, "30")
			in.storePeriod(t, uncapped, "60")
			capRetention(t, caps, capped, 10)
			cappedMonitor := in.monitorID
			uncappedMonitor := in.monitorIn(t, uncapped)
			in.heartbeatsOf(t, cappedMonitor, 90, 45, 15, 5)
			in.heartbeatsOf(t, uncappedMonitor, 90, 45, 15, 5)

			cleanupHeartbeats(in.heartbeats, in.workspaces, in.workspaceSettings, snapshotOf(t, caps), zap.NewNop().Sugar())

			assert.Equal(t, []string{"5d"}, in.heartbeatsOfLeft(t, cappedMonitor), "the 10 day cap is shorter than the 30 day period")
			assert.Equal(t, []string{"45d", "15d", "5d"}, in.heartbeatsOfLeft(t, uncappedMonitor), "a workspace without a cap keeps its own 60 days")

			capRetention(t, caps, uncapped, 20)
			cleanupHeartbeats(in.heartbeats, in.workspaces, in.workspaceSettings, snapshotOf(t, caps), zap.NewNop().Sugar())

			assert.Equal(t, []string{"15d", "5d"}, in.heartbeatsOfLeft(t, uncappedMonitor), "a cap lowered below the period trims on the next run")
			assert.Equal(t, []string{"5d"}, in.heartbeatsOfLeft(t, cappedMonitor))
		})
	}
}

// capsWithDefaults are the workspaces' limits as the install keeps them, with the install's defaults
// from its install settings, as the server wires them (limits.dig.go)
func (in install) capsWithDefaults() (*limits.Chain, *installsettings.Resolver) {
	settings := setting.NewService(setting.NewSQLRepository(in.db), zap.NewNop().Sugar())
	resolver := installsettings.New(&config.Config{InstallSettingsCacheLifetime: time.Nanosecond}, settings, zap.NewNop().Sugar())
	return limits.NewChain(limits.NewSQLStore(in.db), limits.SettingsDefaults{Settings: resolver}), resolver
}

// PP-161: a workspace without limits of its own is capped at the install's default, saved in
// Admin → Settings, and a workspace whose own limits are none is not
func TestCleanupHeartbeatsCapsAWorkspaceWithoutLimitsOfItsOwnAtTheInstallsDefault(t *testing.T) {
	for name, open := range map[string]func(*testing.T) install{"SQLite": sqliteInstall, "Postgres": postgresInstall} {
		t.Run(name, func(t *testing.T) {
			in := open(t)
			caps, resolver := in.capsWithDefaults()
			_, err := resolver.Update(context.Background(), installsettings.Change{WorkspaceLimits: map[string]*int{installsettings.LimitRetentionDays: limits.Of(10)}})
			require.NoError(t, err)
			byDefault := in.workspaceID(t)
			noLimits := workspace.ID(uuid.NewString())
			workspacetest.Add(t, in.db, noLimits, "No limits")
			require.NoError(t, caps.SetOwn(context.Background(), noLimits, &limits.Limits{}))
			in.storePeriod(t, byDefault, "30")
			in.storePeriod(t, noLimits, "30")
			byDefaultMonitor := in.monitorID
			noLimitsMonitor := in.monitorIn(t, noLimits)
			in.heartbeatsOf(t, byDefaultMonitor, 45, 15, 5)
			in.heartbeatsOf(t, noLimitsMonitor, 45, 15, 5)

			cleanupHeartbeats(in.heartbeats, in.workspaces, in.workspaceSettings, snapshotOf(t, caps), zap.NewNop().Sugar())

			assert.Equal(t, []string{"5d"}, in.heartbeatsOfLeft(t, byDefaultMonitor), "the install's 10 day default is shorter than the 30 day period")
			assert.Equal(t, []string{"15d", "5d"}, in.heartbeatsOfLeft(t, noLimitsMonitor), "a workspace whose own limits are none keeps its 30 days")
		})
	}
}

// A cap longer than the period changes nothing: the workspace keeps what its period says
func TestCleanupHeartbeatsKeepsThePeriodWhenTheCapIsLonger(t *testing.T) {
	in := newInstall(t)
	caps := in.caps()
	in.storePeriod(t, in.workspaceID(t), "10")
	capRetention(t, caps, in.workspaceID(t), 60)
	in.heartbeatsOf(t, in.monitorID, 45, 15, 5)

	cleanupHeartbeats(in.heartbeats, in.workspaces, in.workspaceSettings, snapshotOf(t, caps), zap.NewNop().Sugar())

	assert.Equal(t, []string{"5d"}, in.heartbeatsOfLeft(t, in.monitorID))
}

// unreadableCaps is a limits source whose reads fail
type unreadableCaps struct{ limits.Source }

func (unreadableCaps) Snapshot(context.Context) (limits.Snapshot, error) {
	return limits.Snapshot{}, errors.New("database is locked")
}

// When the caps can't be read, each workspace's own period still applies: a cap only ever deletes
// more, so going without one never deletes what a workspace's own period keeps
func TestCleanupHeartbeatsGoesByThePeriodsWhenTheCapsCantBeRead(t *testing.T) {
	in := newInstall(t)
	in.storePeriod(t, in.workspaceID(t), "30")
	in.heartbeatsOf(t, in.monitorID, 45, 15, 5)
	core, logs := observer.New(zap.InfoLevel)

	logger := zap.New(core).Sugar()

	cleanupHeartbeats(in.heartbeats, in.workspaces, in.workspaceSettings, readCaps(context.Background(), unreadableCaps{}, logger), logger)

	assert.Equal(t, []string{"15d", "5d"}, in.heartbeatsOfLeft(t, in.monitorID))
	assert.NotEmpty(t, logs.FilterLevelExact(zap.ErrorLevel).All())
}

// The probes' status changes follow the longest period the workspaces keep in effect, with their
// caps applied
func TestCleanupProbeStatusChangesFollowsTheLongestEffectivePeriod(t *testing.T) {
	in := newInstall(t)
	caps := in.caps()
	in.storePeriod(t, in.workspaceID(t), "100")
	capRetention(t, caps, in.workspaceID(t), 30)
	other := workspace.ID(uuid.NewString())
	workspacetest.Add(t, in.db, other, "Other")
	in.storePeriod(t, other, "20")
	probeID := in.probeHistory(t, "Busy", 200, 150, 120, 60, 40, 10)

	cleanupProbeStatusChanges(in.probes, in.workspaces, in.workspaceSettings, snapshotOf(t, caps), zap.NewNop().Sugar())

	assert.Equal(t, []string{"online 40d", "offline 10d"}, in.probeHistoryLeft(t, probeID), "the first workspace's 100 days are capped at 30, the longest in effect")
}
