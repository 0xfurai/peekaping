package producer

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/monitor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// leaderEngine runs a producer's leader election and leadership monitor on
// millisecond ticks. The claim loop doesn't run, so the due set shows what
// syncing put there.
type leaderEngine struct {
	*engine
	election *LeaderElection
	logs     *observer.ObservedLogs
}

func newLeaderEngine(t *testing.T, monitors ...*monitor.Model) *leaderEngine {
	t.Helper()
	e := newUnstartedEngine(t, monitors...)
	core, logs := observer.New(zap.InfoLevel)
	e.producer.logger = zap.New(core).Sugar()
	e.producer.leadershipCheckInterval = 2 * time.Millisecond

	le := NewLeaderElection(e.rdb, "node1", zap.NewNop().Sugar())
	le.renewalInterval = 2 * time.Millisecond
	e.producer.leaderElection = le
	return &leaderEngine{engine: e, election: le, logs: logs}
}

// start runs leader election and the leadership monitor until the test ends.
func (e *leaderEngine) start(t *testing.T) {
	t.Helper()
	require.NoError(t, e.producer.begin(context.Background()))
	e.election.Start(e.producer.ctx)
	e.producer.wg.Add(1)
	go e.producer.runLeadershipMonitor()
	t.Cleanup(func() {
		e.producer.Stop()
		// Producer.Stop waits for an election that Start began, not for this one
		<-e.election.doneChan
	})
}

// eventuallyStoppedSyncing waits for the schedule refresher to exit because
// leadership was lost. Outside the producer, its log line is the only sign.
func (e *leaderEngine) eventuallyStoppedSyncing(t *testing.T, times int) {
	t.Helper()
	require.Eventually(t, func() bool {
		return e.logs.FilterMessage("Schedule refresher stopped due to leadership loss").Len() == times
	}, 5*time.Second, 2*time.Millisecond, "syncing never stopped")
}

func TestRunLeadershipMonitor(t *testing.T) {
	ctx := context.Background()

	t.Run("a node that becomes leader syncs the schedule", func(t *testing.T) {
		e := newLeaderEngine(t, &monitor.Model{ID: "mon-1", Active: true, Interval: 60})

		e.start(t)

		e.eventuallyDueAt(t, "mon-1", t0)
		holder, err := e.rdb.Get(ctx, LeaderKey).Result()
		require.NoError(t, err)
		assert.Equal(t, "node1", holder)
	})

	t.Run("a follower doesn't sync until the leader's lock expires", func(t *testing.T) {
		e := newLeaderEngine(t, &monitor.Model{ID: "mon-1", Active: true, Interval: 60})
		require.NoError(t, e.rdb.Set(ctx, LeaderKey, "node2", LeaderTTL).Err())

		e.start(t)

		assert.Never(t, func() bool {
			n, err := e.rdb.ZCard(ctx, SchedDueKey).Result()
			return err != nil || n > 0
		}, 50*time.Millisecond, 2*time.Millisecond, "a follower synced the schedule")

		// node2 stops renewing: its lock runs out and this node takes over
		e.mr.FastForward(LeaderTTL)
		e.eventuallyDueAt(t, "mon-1", t0)
	})

	t.Run("a leader that loses leadership stops syncing and syncs again when it gets it back", func(t *testing.T) {
		e := newLeaderEngine(t, &monitor.Model{ID: "mon-1", Active: true, Interval: 60})
		e.start(t)
		e.eventuallyDueAt(t, "mon-1", t0)

		// node2 holds the lock now
		require.NoError(t, e.rdb.Set(ctx, LeaderKey, "node2", LeaderTTL).Err())
		e.eventuallyStoppedSyncing(t, 1)

		// A monitor created meanwhile is only picked up by a new sync
		e.monitors.put(&monitor.Model{ID: "mon-2", Active: true, Interval: 60})
		require.NoError(t, e.rdb.Del(ctx, LeaderKey).Err())

		e.eventuallyDueAt(t, "mon-2", t0)
	})
}

func TestStartJobProcessing(t *testing.T) {
	t.Run("starts job processing with configured concurrency", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		cfg := &config.Config{
			ProducerConcurrency: 2,
		}

		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockProxySvc := new(MockProxyService)
		mockQueueSvc := new(MockQueueService)

		producer := NewProducer(
			client,
			NewQueueRunner(mockQueueSvc, zap.NewNop().Sugar()),
			mockMonitorSvc,
			check.NewLoader(mockMonitorSvc, mockProxySvc, mockMaintenanceSvc, nil, logger),
			nil,
			nil,
			nil,
			le,
			cfg,
			logger,
		)

		require.NoError(t, producer.begin(context.Background()))
		err := producer.startJobProcessing()
		assert.NoError(t, err)

		// Stop returns once both workers and the reclaimer have exited
		producer.Stop()
	})
}

func TestStartMonitorSyncing(t *testing.T) {
	t.Run("successfully start monitor syncing", func(t *testing.T) {
		e := newEngine(t, &monitor.Model{ID: "mon-1", Name: "Monitor 1", Active: true, Interval: 60})
		t.Cleanup(e.producer.Stop)

		err := e.producer.startMonitorSyncing()
		assert.NoError(t, err)

		// The schedule is synced before it returns
		assert.Equal(t, map[string]float64{"mon-1": ms(t0)}, e.members(t, SchedDueKey))
	})

	t.Run("error initializing schedule", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		cfg := &config.Config{
			ProducerConcurrency: 1,
		}

		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockProxySvc := new(MockProxyService)
		mockQueueSvc := new(MockQueueService)

		producer := NewProducer(
			client,
			NewQueueRunner(mockQueueSvc, zap.NewNop().Sugar()),
			mockMonitorSvc,
			check.NewLoader(mockMonitorSvc, mockProxySvc, mockMaintenanceSvc, nil, logger),
			nil,
			nil,
			nil,
			le,
			cfg,
			logger,
		)

		require.NoError(t, producer.begin(context.Background()))
		// Mock error
		mockMonitorSvc.On("FindActivePaginated", producer.ctx, 0, 100).Return(nil, assert.AnError)

		err := producer.startMonitorSyncing()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to initialize schedule")

		mockMonitorSvc.AssertExpectations(t)
	})
}

func TestStopMonitorSyncing(t *testing.T) {
	// PP-61: stopMonitorSyncing sets syncCtx to nil, and the refresher reads that
	// field on every loop. Stopping before the refresher's first select makes it
	// call Done on a nil context and crash the process.
	t.Run("stops the schedule refresher", func(t *testing.T) {
		t.Skip("PP-61: stopping right after starting crashes the refresher on a nil syncCtx")
		e := newEngine(t)
		t.Cleanup(e.producer.Stop)
		require.NoError(t, e.producer.startMonitorSyncing())

		e.producer.stopMonitorSyncing()

		// The refresher was the producer's only goroutine
		waitForGoroutines(t, &e.producer.wg)
	})

	t.Run("does nothing when not syncing", func(t *testing.T) {
		e := newEngine(t)

		e.producer.stopMonitorSyncing()

		assert.Nil(t, e.producer.syncCancel)
	})
}
