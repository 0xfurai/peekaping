package cleanup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stats"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// install is a SQLite database with the heartbeat, user and setting stores the
// cleanup runs on, and one monitor to record heartbeats for.
type install struct {
	db         *bun.DB
	heartbeats heartbeat.Service
	users      auth.Repository
	settings   setting.Service
	monitorID  string
}

func newInstall(t *testing.T) install {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "cleanup.db")}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ctx := context.Background()
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err = migrator.Migrate(ctx)
	require.NoError(t, err)
	monitorID := uuid.NewString()
	_, err = db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, monitorID)
	require.NoError(t, err)
	return install{
		db:         db,
		heartbeats: heartbeat.NewService(heartbeat.NewSQLRepository(db), nil, zap.NewNop().Sugar()),
		users:      auth.NewSQLRepository(db),
		settings:   setting.NewService(setting.NewSQLRepository(db), zap.NewNop().Sugar()),
		monitorID:  monitorID,
	}
}

// registerAdmin creates the install's admin, as its first setup does.
func (in install) registerAdmin(t *testing.T) {
	t.Helper()
	_, err := in.users.Create(context.Background(), &auth.Model{Email: "admin@example.com", Password: "hash", Active: true})
	require.NoError(t, err)
}

type heartbeatRow struct {
	bun.BaseModel `bun:"table:heartbeats"`

	ID        string    `bun:"id,pk"`
	MonitorID string    `bun:"monitor_id"`
	Status    int       `bun:"status"`
	Msg       string    `bun:"msg"`
	Time      time.Time `bun:"time"`
}

// recordHeartbeats stores a heartbeat for each age in days, oldest first like
// the ingester writes them, with the age as its message.
func (in install) recordHeartbeats(t *testing.T, ages ...int) {
	t.Helper()
	now := time.Now().UTC()
	for _, days := range ages {
		row := &heartbeatRow{ID: uuid.NewString(), MonitorID: in.monitorID, Status: 1, Msg: fmt.Sprintf("%dd", days), Time: now.AddDate(0, 0, -days)}
		_, err := in.db.NewInsert().Model(row).Exec(context.Background())
		require.NoError(t, err)
	}
}

// heartbeatsLeft returns the messages of the monitor's heartbeats, oldest first.
func (in install) heartbeatsLeft(t *testing.T) []string {
	t.Helper()
	left, err := in.heartbeats.FindByMonitorIDPaginated(context.Background(), in.monitorID, 100, 0, nil, true)
	require.NoError(t, err)
	msgs := []string{}
	for _, hb := range left {
		msgs = append(msgs, hb.Msg)
	}
	return msgs
}

// storedPeriod returns the stored data retention period, or "" if none is stored.
func (in install) storedPeriod(t *testing.T) string {
	t.Helper()
	stored, err := in.settings.GetByKey(context.Background(), setting.DataRetentionPeriodKey)
	require.NoError(t, err)
	if stored == nil {
		return ""
	}
	assert.Equal(t, "int", stored.Type)
	return stored.Value
}

func (in install) storePeriod(t *testing.T, value string) {
	t.Helper()
	_, err := in.settings.SetByKey(context.Background(), setting.DataRetentionPeriodKey, &setting.Model{Value: value, Type: "int"})
	require.NoError(t, err)
}

func TestUpgradedInstallKeepsAYearOfHeartbeats(t *testing.T) {
	in := newInstall(t)
	in.recordHeartbeats(t, 400, 300, 1)
	logger := zap.NewNop().Sugar()

	initDataRetentionPeriod(in.heartbeats, in.users, in.settings, logger)
	cleanupHeartbeats(in.heartbeats, in.settings, logger)

	assert.Equal(t, "", in.storedPeriod(t), "an install that never saved a period keeps none")
	assert.Equal(t, []string{"300d", "1d"}, in.heartbeatsLeft(t))
}

func TestUpgradedInstallWithNoHeartbeatsLeftKeepsAYearOfHeartbeats(t *testing.T) {
	// Every monitor was deleted, which deleted its heartbeats, but the install
	// was set up before 180 days became the default.
	in := newInstall(t)
	in.registerAdmin(t)

	initDataRetentionPeriod(in.heartbeats, in.users, in.settings, zap.NewNop().Sugar())

	assert.Equal(t, "", in.storedPeriod(t))
}

func TestNewInstallStoresA180DayRetentionPeriod(t *testing.T) {
	in := newInstall(t)
	logger := zap.NewNop().Sugar()

	initDataRetentionPeriod(in.heartbeats, in.users, in.settings, logger)
	require.Equal(t, "180", in.storedPeriod(t))

	in.recordHeartbeats(t, 200, 179, 1)
	initDataRetentionPeriod(in.heartbeats, in.users, in.settings, logger)
	cleanupHeartbeats(in.heartbeats, in.settings, logger)
	assert.Equal(t, "180", in.storedPeriod(t))
	assert.Equal(t, []string{"179d", "1d"}, in.heartbeatsLeft(t))
}

func TestInitDataRetentionPeriodKeepsAStoredPeriod(t *testing.T) {
	for name, heartbeats := range map[string][]int{"new install": nil, "install with heartbeats": {10}} {
		t.Run(name, func(t *testing.T) {
			in := newInstall(t)
			in.recordHeartbeats(t, heartbeats...)
			in.storePeriod(t, "30")

			initDataRetentionPeriod(in.heartbeats, in.users, in.settings, zap.NewNop().Sugar())

			assert.Equal(t, "30", in.storedPeriod(t))
		})
	}
}

// brokenHeartbeats is a heartbeat service that can't tell whether heartbeats exist.
type brokenHeartbeats struct{ heartbeat.Service }

func (brokenHeartbeats) HasAny(context.Context) (bool, error) {
	return false, errors.New("database is locked")
}

func TestInitDataRetentionPeriodStoresNothingWhenItCantCheckForHeartbeats(t *testing.T) {
	in := newInstall(t)
	core, logs := observer.New(zap.InfoLevel)

	initDataRetentionPeriod(brokenHeartbeats{in.heartbeats}, in.users, in.settings, zap.New(core).Sugar())

	assert.Equal(t, "", in.storedPeriod(t))
	require.Len(t, logs.FilterLevelExact(zap.ErrorLevel).All(), 1)
}

// uncountableUsers is a user repository that can't count users.
type uncountableUsers struct{ auth.Repository }

func (uncountableUsers) FindAllCount(context.Context) (int64, error) {
	return 0, errors.New("database is locked")
}

func TestInitDataRetentionPeriodStoresNothingWhenItCantCountUsers(t *testing.T) {
	in := newInstall(t)
	core, logs := observer.New(zap.InfoLevel)

	initDataRetentionPeriod(in.heartbeats, uncountableUsers{in.users}, in.settings, zap.New(core).Sugar())

	assert.Equal(t, "", in.storedPeriod(t))
	require.Len(t, logs.FilterLevelExact(zap.ErrorLevel).All(), 1)
}

// unreadableSettings is a setting service whose reads fail.
type unreadableSettings struct{ setting.Service }

func (unreadableSettings) GetByKey(context.Context, string) (*setting.Model, error) {
	return nil, errors.New("database is locked")
}

func TestInitDataRetentionPeriodStoresNothingWhenItCantReadThePeriod(t *testing.T) {
	in := newInstall(t)
	core, logs := observer.New(zap.InfoLevel)

	initDataRetentionPeriod(in.heartbeats, in.users, unreadableSettings{in.settings}, zap.New(core).Sugar())

	assert.Equal(t, "", in.storedPeriod(t))
	require.Len(t, logs.FilterLevelExact(zap.ErrorLevel).All(), 1)
}

func TestCleanupHeartbeatsSkipsAnUnusablePeriod(t *testing.T) {
	for name, value := range map[string]string{
		"zero":        "0",
		"negative":    "-5",
		"not numeric": "abc",
		"over 36,500": "36501",
		"overflowing": "4611686018427387903",
	} {
		t.Run(name, func(t *testing.T) {
			in := newInstall(t)
			in.recordHeartbeats(t, 400, 1)
			// Stored before the settings API checked values.
			in.storePeriod(t, value)
			core, logs := observer.New(zap.InfoLevel)

			cleanupHeartbeats(in.heartbeats, in.settings, zap.New(core).Sugar())

			assert.Equal(t, []string{"400d", "1d"}, in.heartbeatsLeft(t))
			entries := logs.FilterLevelExact(zap.ErrorLevel).All()
			require.Len(t, entries, 1)
			assert.Equal(t, value, entries[0].ContextMap()["value"])
		})
	}

	t.Run("unreadable", func(t *testing.T) {
		in := newInstall(t)
		in.recordHeartbeats(t, 400, 1)
		core, logs := observer.New(zap.InfoLevel)

		cleanupHeartbeats(in.heartbeats, unreadableSettings{in.settings}, zap.New(core).Sugar())

		assert.Equal(t, []string{"400d", "1d"}, in.heartbeatsLeft(t))
		require.Len(t, logs.FilterLevelExact(zap.ErrorLevel).All(), 1)
	})
}

// expiringStats is a stats service whose DeleteExpired returns fixed results.
type expiringStats struct {
	stats.Service
	deleted map[stats.StatPeriod]int64
	err     error
}

func (s expiringStats) DeleteExpired(context.Context, time.Time) (map[stats.StatPeriod]int64, error) {
	return s.deleted, s.err
}

func TestCleanupStatsLogsRowsDeletedPerPeriod(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	deleted := map[stats.StatPeriod]int64{stats.StatMinutely: 6000, stats.StatHourly: 100}

	cleanupStats(expiringStats{deleted: deleted}, zap.New(core).Sugar())

	entries := logs.FilterMessage("Deleted expired stats").All()
	require.Len(t, entries, 1)
	assert.Equal(t, deleted, entries[0].ContextMap()["count"])
}

func TestCleanupStatsLogsAFailure(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	deleted := map[stats.StatPeriod]int64{stats.StatMinutely: 5000}

	cleanupStats(expiringStats{deleted: deleted, err: errors.New("database is locked")}, zap.New(core).Sugar())

	entries := logs.FilterMessage("Failed to delete expired stats").All()
	require.Len(t, entries, 1)
	assert.Equal(t, zap.ErrorLevel, entries[0].Level)
	assert.Equal(t, "database is locked", entries[0].ContextMap()["error"])
	assert.Equal(t, deleted, entries[0].ContextMap()["count"])
	assert.Empty(t, logs.FilterMessage("Deleted expired stats").All())
}

func TestStartCleanupCronStopsWhenItsCronIsStopped(t *testing.T) {
	in := newInstall(t)

	// The hourly jobs don't run in the test, so they need no stats, history or TLS services
	c := StartCleanupCron(in.heartbeats, in.users, in.settings, nil, nil, nil, zap.NewNop().Sugar())
	require.Len(t, c.Entries(), 4, "the cleanup jobs are scheduled")

	select {
	case <-c.Stop().Done():
	case <-time.After(time.Second):
		require.FailNow(t, "the cron didn't stop")
	}
	assert.Equal(t, "180", in.storedPeriod(t), "it stored the new install's data retention period first")
}
