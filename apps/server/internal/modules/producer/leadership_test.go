package producer

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/leader"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

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
	election *leader.Elector
	logs     *observer.ObservedLogs
}

func newLeaderEngine(t *testing.T, monitors ...*monitor.Model) *leaderEngine {
	t.Helper()
	e := newUnstartedEngine(t, monitors...)
	core, logs := observer.New(zap.InfoLevel)
	e.producer.logger = zap.New(core).Sugar()
	e.producer.leadershipCheckInterval = 2 * time.Millisecond

	// The election logs to the same observer as the producer: the leadership monitor's log lines
	// are the election's now
	le := leader.New(e.rdb, "node1", zap.New(core).Sugar(), leader.WithRenewalInterval(2*time.Millisecond))
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
		<-e.election.Done()
	})
}

// startSyncing starts the producer's monitor syncing, as the leadership monitor does when the node
// gets the leadership, and returns what the loss of the leadership does to it: it ends syncing.
func startSyncing(t *testing.T, p *Producer) (stop context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(p.ctx)
	t.Cleanup(cancel)
	require.NoError(t, p.startMonitorSyncing(ctx))
	return cancel
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
		holder, err := e.rdb.Get(ctx, leader.Key).Result()
		require.NoError(t, err)
		assert.Equal(t, "node1", holder)
	})

	t.Run("a follower doesn't sync until the leader's lock expires", func(t *testing.T) {
		e := newLeaderEngine(t, &monitor.Model{ID: "mon-1", Active: true, Interval: 60})
		require.NoError(t, e.rdb.Set(ctx, leader.Key, "node2", leader.TTL).Err())

		e.start(t)

		assert.Never(t, func() bool {
			n, err := e.rdb.ZCard(ctx, SchedDueKey).Result()
			return err != nil || n > 0
		}, 50*time.Millisecond, 2*time.Millisecond, "a follower synced the schedule")

		// node2 stops renewing: its lock runs out and this node takes over
		e.mr.FastForward(leader.TTL)
		e.eventuallyDueAt(t, "mon-1", t0)
	})

	t.Run("a leader that loses leadership stops syncing and syncs again when it gets it back", func(t *testing.T) {
		e := newLeaderEngine(t, &monitor.Model{ID: "mon-1", Active: true, Interval: 60})
		e.start(t)
		e.eventuallyDueAt(t, "mon-1", t0)

		// node2 holds the lock now
		require.NoError(t, e.rdb.Set(ctx, leader.Key, "node2", leader.TTL).Err())
		e.eventuallyStoppedSyncing(t, 1)

		// A monitor created meanwhile is only picked up by a new sync
		e.monitors.put(&monitor.Model{ID: "mon-2", Active: true, Interval: 60})
		require.NoError(t, e.rdb.Del(ctx, leader.Key).Err())

		e.eventuallyDueAt(t, "mon-2", t0)
	})
}

func TestStartJobProcessing(t *testing.T) {
	t.Run("starts job processing with configured concurrency", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := leader.New(client, "node1", logger)

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

		err := e.producer.startMonitorSyncing(e.producer.ctx)
		assert.NoError(t, err)

		// The schedule is synced before it returns
		assert.Equal(t, map[string]float64{"mon-1": ms(t0)}, e.members(t, SchedDueKey))
	})

	t.Run("error initializing schedule", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := leader.New(client, "node1", logger)

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
		mockMonitorSvc.On("FindActiveByProbe", producer.ctx, shared.LocalProbeID, 0, 100).Return(nil, assert.AnError)

		err := producer.startMonitorSyncing(producer.ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to initialize schedule")

		mockMonitorSvc.AssertExpectations(t)
	})
}

// Syncing ends when the node loses the leadership, which cancels the context it was started with
func TestStartMonitorSyncing_EndsWithItsContext(t *testing.T) {
	e := newEngine(t)
	t.Cleanup(e.producer.Stop)
	stopSyncing := startSyncing(t, e.producer)

	stopSyncing()

	// The refresher is the producer's only goroutine but the watchdog
	waitForGoroutines(t, &e.producer.wg)
	waitForGoroutines(t, &e.producer.watchdogWg)
}
