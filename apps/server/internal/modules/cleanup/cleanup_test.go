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
	"peekaping/internal/dbtx"
	"peekaping/internal/infra"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe_notification"
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
	probes     probe.Service
	probeRepo  probe.Repository
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
	logger := zap.NewNop().Sugar()
	probeRepo := probe.NewSQLRepository(db)
	return install{
		db:         db,
		probeRepo:  probeRepo,
		probes:     probe.NewService(probeRepo, dbtx.NewSQLTransactor(db), probe_notification.NewService(probe_notification.NewSQLRepository(db), logger), logger),
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

// probeHistory makes a probe whose status changes happened the given days ago, oldest first, the
// probe going online first and offline after that. It returns the probe's id.
func (in install) probeHistory(t *testing.T, name string, ages ...int) string {
	t.Helper()
	ctx := context.Background()
	created, err := in.probeRepo.Create(ctx, &probe.CreateModel{Name: name, TokenHash: "sha256$" + name, TokenPrefix: "pkp_" + name})
	require.NoError(t, err)
	_, err = in.db.Exec("DELETE FROM probe_status_changes WHERE probe_id = ?", created.ID)
	require.NoError(t, err)
	from, to := probe.StatusOffline, probe.StatusOnline
	for _, days := range ages {
		changed, err := in.probeRepo.UpdateStatus(ctx, created.ID, from, to, time.Now().UTC().AddDate(0, 0, -days))
		require.NoError(t, err)
		require.True(t, changed)
		from, to = to, from
	}
	return created.ID
}

// probeHistoryLeft returns the probe's status changes as "status age in days", oldest first.
func (in install) probeHistoryLeft(t *testing.T, probeID string) []string {
	t.Helper()
	changes, err := in.probes.FindStatusChanges(context.Background(), probeID, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	left := []string{}
	for _, c := range changes {
		left = append(left, fmt.Sprintf("%s %dd", c.Status, int(time.Since(c.At).Hours()/24+0.5)))
	}
	return left
}

func TestCleanupProbeStatusChangesKeepsEachProbesLatestChangeBeforeTheRetentionPeriod(t *testing.T) {
	in := newInstall(t)
	in.storePeriod(t, "30")
	busy := in.probeHistory(t, "Busy", 100, 50, 40, 10)
	steady := in.probeHistory(t, "Steady", 200)
	logger := zap.NewNop().Sugar()

	cleanupProbeStatusChanges(in.probes, in.settings, logger)

	assert.Equal(t, []string{"online 40d", "offline 10d"}, in.probeHistoryLeft(t, busy), "the changes 100 and 50 days old go; the 40 days old one is the latest before the 30 day cutoff, so it stays")
	assert.Equal(t, []string{"online 200d"}, in.probeHistoryLeft(t, steady), "a probe that has not changed since keeps the change that made it so")
}

func TestCleanupProbeStatusChangesKeepsAYearWhenNoPeriodIsStored(t *testing.T) {
	in := newInstall(t)
	probeID := in.probeHistory(t, "Old", 500, 400, 300)

	cleanupProbeStatusChanges(in.probes, in.settings, zap.NewNop().Sugar())

	assert.Equal(t, []string{"offline 400d", "online 300d"}, in.probeHistoryLeft(t, probeID))
}

func TestCleanupProbeStatusChangesSkipsAnUnusablePeriod(t *testing.T) {
	in := newInstall(t)
	probeID := in.probeHistory(t, "Old", 500, 400, 300)
	in.storePeriod(t, "abc")
	core, logs := observer.New(zap.InfoLevel)

	cleanupProbeStatusChanges(in.probes, in.settings, zap.New(core).Sugar())

	assert.Equal(t, []string{"online 500d", "offline 400d", "online 300d"}, in.probeHistoryLeft(t, probeID))
	require.Len(t, logs.FilterLevelExact(zap.ErrorLevel).All(), 1)

	cleanupProbeStatusChanges(in.probes, unreadableSettings{in.settings}, zap.New(core).Sugar())
	assert.Len(t, in.probeHistoryLeft(t, probeID), 3)
	assert.Len(t, logs.FilterLevelExact(zap.ErrorLevel).All(), 2)
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

	// The hourly job doesn't run in the test, so it needs no stats, history or TLS services
	c, err := StartCleanupCron(in.heartbeats, in.users, in.settings, nil, nil, nil, nil, NewMemoryRunLock(), testRetention, zap.NewNop().Sugar())
	require.NoError(t, err)
	require.Len(t, c.Entries(), 1, "the hourly cleanup is scheduled")

	select {
	case <-c.Stop().Done():
	case <-time.After(time.Second):
		require.FailNow(t, "the cron didn't stop")
	}
	assert.Equal(t, "180", in.storedPeriod(t), "it stored the new install's data retention period first")
}

// heldLock is a run lock whose hour another replica has claimed
type heldLock struct{}

func (heldLock) Claim(context.Context, time.Time) (bool, error) { return false, nil }

// unreachableLock is a run lock that can't reach its Redis
type unreachableLock struct{}

func (unreachableLock) Claim(context.Context, time.Time) (bool, error) {
	return false, errors.New("connection refused")
}

func TestStartCleanupCronsJobRunsTheHourlyCleanupUnderTheLock(t *testing.T) {
	for name, tc := range map[string]struct {
		lock RunLock
		want [5]int32
	}{
		"the lock grants the hour":       {NewMemoryRunLock(), [5]int32{1, 1, 1, 1, 1}},
		"another replica holds the hour": {heldLock{}, [5]int32{}},
		"the lock can't be reached":      {unreachableLock{}, [5]int32{}},
	} {
		t.Run(name, func(t *testing.T) {
			in := newInstall(t)
			c := &cleanups{}
			retention := Retention{NotificationHistoryDays: 45, TLSInfoDays: 7}

			cron, err := StartCleanupCron(countingHeartbeats{Service: in.heartbeats, c: c}, in.users, in.settings,
				countingStats{c: c}, countingNotificationHistory{c: c}, countingTLSInfo{c: c}, countingProbes{Service: in.probes, c: c}, tc.lock, retention, zap.NewNop().Sugar())
			require.NoError(t, err)
			t.Cleanup(func() { <-cron.Stop().Done() })
			require.Len(t, cron.Entries(), 1)

			// The cron runs this when the hour starts
			cron.Entries()[0].WrappedJob.Run()

			assert.Equal(t, tc.want, c.all())
			if tc.want[2] == 1 {
				assert.Equal(t, int32(45), c.notificationHistoryDays.Load())
				assert.Equal(t, int32(7), c.tlsInfoDays.Load())
			}
		})
	}
}

func TestStartCleanupCronFailsWhenItCantScheduleTheCleanup(t *testing.T) {
	in := newInstall(t)
	spec := hourlySpec
	hourlySpec = "not a cron spec"
	t.Cleanup(func() { hourlySpec = spec })

	c, err := StartCleanupCron(in.heartbeats, in.users, in.settings, nil, nil, nil, nil, NewMemoryRunLock(), testRetention, zap.NewNop().Sugar())

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to schedule the hourly cleanup")
	assert.Nil(t, c)
}

// A time zone that is offset by part of an hour has its top of the hour inside a UTC
// hour, and there the nearest-hour rounding of the claims would split replicas by a moment's
// skew. The cron fires at the top of the UTC hour whatever TZ is.
func TestStartCleanupCronFiresAtTheTopOfTheHourInUTC(t *testing.T) {
	in := newInstall(t)

	c, err := StartCleanupCron(in.heartbeats, in.users, in.settings, nil, nil, nil, nil, NewMemoryRunLock(), testRetention, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { <-c.Stop().Done() })

	assert.Equal(t, "UTC", c.Location().String())
	require.Len(t, c.Entries(), 1)
	// From 11:17 in India (05:47 UTC), the next run is at 06:00 UTC, 11:30 in India. The cron
	// asks its schedule with the time in its own location, as here.
	india := time.FixedZone("IST", 5*3600+30*60)
	next := c.Entries()[0].Schedule.Next(time.Date(2026, 10, 22, 11, 17, 0, 0, india).In(c.Location()))
	assert.Equal(t, time.Date(2026, 10, 22, 6, 0, 0, 0, time.UTC), next.UTC())
}
