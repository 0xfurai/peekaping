package cleanup

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stats"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// cleanups counts the cleanups that the replicas sharing it run, and the number of days the
// latest notification history and TLS info cleanups were given
type cleanups struct {
	heartbeats, stats, notificationHistory, tlsInfo, probeStatusChanges atomic.Int32

	notificationHistoryDays, tlsInfoDays atomic.Int32
}

// all returns how many times each cleanup ran, in the order of runHourly
func (c *cleanups) all() [5]int32 {
	return [5]int32{c.heartbeats.Load(), c.stats.Load(), c.notificationHistory.Load(), c.tlsInfo.Load(), c.probeStatusChanges.Load()}
}

type countingHeartbeats struct {
	heartbeat.Service
	c *cleanups
}

func (s countingHeartbeats) DeleteOlderThan(context.Context, time.Time) (int64, error) {
	s.c.heartbeats.Add(1)
	return 0, nil
}

type countingStats struct {
	stats.Service
	c *cleanups
}

func (s countingStats) DeleteExpired(context.Context, time.Time) (map[stats.StatPeriod]int64, error) {
	s.c.stats.Add(1)
	return nil, nil
}

type countingNotificationHistory struct {
	notification_sent_history.Service
	c *cleanups
}

func (s countingNotificationHistory) CleanupOldRecords(_ context.Context, olderThanDays int) error {
	s.c.notificationHistory.Add(1)
	s.c.notificationHistoryDays.Store(int32(olderThanDays))
	return nil
}

type countingTLSInfo struct {
	monitor_tls_info.Service
	c *cleanups
}

func (s countingTLSInfo) CleanupOldRecords(_ context.Context, olderThanDays int) error {
	s.c.tlsInfo.Add(1)
	s.c.tlsInfoDays.Store(int32(olderThanDays))
	return nil
}

type countingProbes struct {
	probe.Service
	c *cleanups
}

func (s countingProbes) DeleteStatusChangesOlderThan(context.Context, time.Time) (int, error) {
	s.c.probeStatusChanges.Add(1)
	return 0, nil
}

// noStoredSettings is a setting service with no stored data retention period
type noStoredSettings struct{ setting.Service }

func (noStoredSettings) GetByKey(context.Context, string) (*setting.Model, error) {
	return nil, nil
}

// testRetention is the retention the replicas of these tests run with
var testRetention = Retention{NotificationHistoryDays: 90, TLSInfoDays: 30}

// newReplica is one Peekaping process: a cleanup on the lock, counting what it runs in c
func newReplica(c *cleanups, lock RunLock, logger *zap.SugaredLogger) *hourlyCleanup {
	return &hourlyCleanup{
		heartbeats:          countingHeartbeats{c: c},
		settings:            noStoredSettings{},
		stats:               countingStats{c: c},
		notificationHistory: countingNotificationHistory{c: c},
		tlsInfo:             countingTLSInfo{c: c},
		probes:              countingProbes{c: c},
		lock:                lock,
		retention:           testRetention,
		logger:              logger,
	}
}

// redisLock is an api replica's lock on the Redis at addr. Every replica has a client of its own.
func redisLock(t *testing.T, addr string) RunLock {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewRedisRunLock(rdb)
}

var noon = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func TestHourlyCleanupRunsOnceAcrossTheReplicas(t *testing.T) {
	redis := miniredis.RunT(t)
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core).Sugar()
	c := &cleanups{}
	replicas := []*hourlyCleanup{
		newReplica(c, redisLock(t, redis.Addr()), logger),
		newReplica(c, redisLock(t, redis.Addr()), logger),
		newReplica(c, redisLock(t, redis.Addr()), logger),
	}

	// Every replica's cron fires at the top of the hour
	runAll := func(now time.Time) {
		var wg sync.WaitGroup
		for _, replica := range replicas {
			wg.Add(1)
			go func() {
				defer wg.Done()
				replica.runHourly(context.Background(), now)
			}()
		}
		wg.Wait()
	}

	runAll(noon)

	assert.Equal(t, [5]int32{1, 1, 1, 1, 1}, c.all(), "cleanups run by three replicas in one hour")
	running := logs.FilterMessage("Running the hourly cleanup").All()
	require.Len(t, running, 1, "replicas that run the cleanup")
	assert.Equal(t, zap.InfoLevel, running[0].Level)
	assert.Equal(t, "2026-10-22T12:00:00Z", running[0].ContextMap()["hour"])
	others := logs.FilterMessage("Another replica runs the hourly cleanup").All()
	require.Len(t, others, 2, "replicas that leave it to another")
	for _, entry := range others {
		assert.Equal(t, zap.DebugLevel, entry.Level)
		assert.Equal(t, "2026-10-22T12:00:00Z", entry.ContextMap()["hour"])
	}
	assert.Empty(t, logs.FilterLevelExact(zap.ErrorLevel).All())

	runAll(noon.Add(time.Hour))

	assert.Equal(t, [5]int32{2, 2, 2, 2, 2}, c.all(), "the next hour runs once again")
	running = logs.FilterMessage("Running the hourly cleanup").All()
	require.Len(t, running, 2)
	assert.Equal(t, "2026-10-22T13:00:00Z", running[1].ContextMap()["hour"])
}

// A timer wakes a little early, or a run starts late. Either way it claims the hour it was
// scheduled for: now is rounded to the nearest one.
func TestHourlyCleanupClaimsItsHourWhenTheCronFiresEarlyOrLate(t *testing.T) {
	redis := miniredis.RunT(t)
	c := &cleanups{}
	logger := zap.NewNop().Sugar()
	ctx := context.Background()

	// Within half an hour of noon, either side
	for _, now := range []time.Time{
		noon.Add(-30 * time.Minute),
		noon.Add(-2 * time.Second),
		noon,
		noon.Add(2 * time.Second),
		noon.Add(30*time.Minute - time.Nanosecond),
	} {
		newReplica(c, redisLock(t, redis.Addr()), logger).runHourly(ctx, now)
	}
	assert.Equal(t, [5]int32{1, 1, 1, 1, 1}, c.all(), "cleanups run for the hour around noon")

	// Half an hour past noon is the next hour's
	newReplica(c, redisLock(t, redis.Addr()), logger).runHourly(ctx, noon.Add(30*time.Minute))
	assert.Equal(t, [5]int32{2, 2, 2, 2, 2}, c.all(), "cleanups run once the next hour's replicas fire")
}

func TestHourlyCleanupSkipsTheHourWhenRedisCantBeReached(t *testing.T) {
	redis := miniredis.RunT(t)
	core, logs := observer.New(zap.DebugLevel)
	c := &cleanups{}
	replica := newReplica(c, redisLock(t, redis.Addr()), zap.New(core).Sugar())
	ctx := context.Background()
	redis.Close()

	replica.runHourly(ctx, noon)

	assert.Equal(t, [5]int32{}, c.all(), "cleanups run without the lock")
	errs := logs.FilterLevelExact(zap.ErrorLevel).All()
	require.Len(t, errs, 1)
	assert.Equal(t, "Failed to claim the hourly cleanup, skipping it this hour", errs[0].Message)
	assert.Equal(t, "2026-10-22T12:00:00Z", errs[0].ContextMap()["hour"])
	assert.Contains(t, errs[0].ContextMap(), "error")
	assert.Empty(t, logs.FilterMessage("Running the hourly cleanup").All())

	// The next hour tries again
	require.NoError(t, redis.Restart())
	replica.runHourly(ctx, noon.Add(time.Hour))

	assert.Equal(t, [5]int32{1, 1, 1, 1, 1}, c.all(), "cleanups run once Redis is back")
}

func TestHourlyCleanupClaimsTheHourForTwoHoursInRedis(t *testing.T) {
	redis := miniredis.RunT(t)
	replica := newReplica(&cleanups{}, redisLock(t, redis.Addr()), zap.NewNop().Sugar())

	replica.runHourly(context.Background(), noon.Add(-time.Second))

	assert.True(t, redis.Exists("peekaping:cleanup:2026-10-22T12"))
	assert.Equal(t, 2*time.Hour, redis.TTL("peekaping:cleanup:2026-10-22T12"))
}

func TestHourlyCleanupKeepsNotificationHistoryAndTLSInfoForTheirRetention(t *testing.T) {
	c := &cleanups{}
	replica := newReplica(c, NewMemoryRunLock(), zap.NewNop().Sugar())
	replica.retention = Retention{NotificationHistoryDays: 45, TLSInfoDays: 7}

	replica.runHourly(context.Background(), noon)

	assert.Equal(t, int32(45), c.notificationHistoryDays.Load(), "days notification history is kept")
	assert.Equal(t, int32(7), c.tlsInfoDays.Load(), "days TLS info is kept")
}

func TestHourlyCleanupInSingleModeRunsEveryHour(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	c := &cleanups{}
	replica := newReplica(c, NewMemoryRunLock(), zap.New(core).Sugar())

	for hours := range 3 {
		replica.runHourly(context.Background(), noon.Add(time.Duration(hours)*time.Hour))
	}

	assert.Equal(t, [5]int32{3, 3, 3, 3, 3}, c.all())
	assert.Len(t, logs.FilterMessage("Running the hourly cleanup").All(), 3)
	assert.Empty(t, logs.FilterMessage("Another replica runs the hourly cleanup").All())
}
