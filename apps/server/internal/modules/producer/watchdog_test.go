package producer

import (
	"context"
	"errors"
	"testing"
	"time"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/worker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewProducer_HasAWatchdog(t *testing.T) {
	e := newEngine(t)

	assert.NotNil(t, e.producer.watchdog)
}

func TestWatchdog_RunsOnlyWhileLeader(t *testing.T) {
	e := newEngine(t)
	p := e.producer
	p.scheduleRefreshInterval = time.Hour
	store := stale.NewRedisStore(e.rdb)
	p.watchdog = stale.NewWatchdog(stale.WatchdogConfig{
		Store:        store,
		Rechecker:    p,
		Monitors:     e.monitors,
		CheckTimeout: worker.HealthCheckTaskTimeout,
		Interval:     20 * time.Millisecond,
		Logger:       zap.NewNop().Sugar(),
	})
	defer p.cancel()

	require.NoError(t, p.startMonitorSyncing())
	require.Eventually(t, func() bool {
		ranAt, _ := store.RanAt(context.Background())
		return ranAt != nil
	}, 2*time.Second, 10*time.Millisecond)

	p.stopMonitorSyncing()
	p.wg.Wait()
	p.watchdogWg.Wait()
	require.NoError(t, e.rdb.Del(context.Background(), stale.RanAtKey).Err())
	time.Sleep(100 * time.Millisecond)
	ranAt, err := store.RanAt(context.Background())
	require.NoError(t, err)
	assert.Nil(t, ranAt, "the watchdog stopped with leadership")
}

func TestRunNow(t *testing.T) {
	ctx := context.Background()

	t.Run("schedules the monitor to run right away", func(t *testing.T) {
		e := newEngine(t, &monitor.Model{ID: "monitor-123", Active: true, Interval: 60})
		// Scheduled for its next slot, with a lease held on it
		e.producer.monitorIntervals["monitor-123"] = 60
		e.addDue(t, "monitor-123", t0.Add(30*time.Second))
		e.addLease(t, "monitor-123", t0.Add(LeaseTTL))

		require.NoError(t, e.producer.RunNow(ctx, "monitor-123"))

		assert.Equal(t, map[string]float64{"monitor-123": ms(t0)}, e.members(t, SchedDueKey))
		assert.Empty(t, e.members(t, SchedLeaseKey))
	})

	t.Run("fails for a monitor that doesn't exist", func(t *testing.T) {
		e := newEngine(t)

		assert.Error(t, e.producer.RunNow(ctx, "monitor-123"))
		assert.Empty(t, e.members(t, SchedDueKey))
	})

	t.Run("fails when the monitor can't be read", func(t *testing.T) {
		e := newEngine(t, &monitor.Model{ID: "monitor-123", Active: true, Interval: 60})
		readErr := errors.New("database error")
		e.monitors.failNextFind("monitor-123", readErr)

		assert.ErrorIs(t, e.producer.RunNow(ctx, "monitor-123"), readErr)
		assert.Empty(t, e.members(t, SchedDueKey))
	})
}
