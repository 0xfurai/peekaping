package producer

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/leader"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/monitor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestEventListener_NewEventListener(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()

	logger := zap.NewNop().Sugar()
	le := leader.New(client, "node1", logger)
	producer := &Producer{leaderElection: le}

	eventListener := NewEventListener(producer, logger)

	assert.NotNil(t, eventListener)
	assert.Equal(t, producer, eventListener.producer)
}

func TestEventListener_Subscribe(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()

	logger := zap.NewNop().Sugar()
	le := leader.New(client, "node1", logger)
	producer := &Producer{leaderElection: le}

	eventListener := NewEventListener(producer, logger)
	eventBus := eventstest.NewBus()

	eventListener.Subscribe(eventBus)

	assert.ElementsMatch(t,
		[]string{events.MonitorCreated.Name(), events.MonitorUpdated.Name(), events.MonitorDeleted.Name()},
		eventBus.SubscribedTopics())
}

// newListener subscribes an event listener on a producer that is the leader,
// or a follower of node2, and returns the bus it listens on.
func newListener(t *testing.T, isLeader bool, monitors ...*monitor.Model) (*engine, *eventstest.Bus) {
	t.Helper()
	ctx := context.Background()
	e := newEngine(t, monitors...)
	if !isLeader {
		require.NoError(t, e.rdb.Set(ctx, leader.Key, "node2", leader.TTL).Err())
	}
	le := leader.New(e.rdb, "node1", zap.NewNop().Sugar())
	le.Start(ctx)
	t.Cleanup(le.Stop)
	if isLeader {
		require.Eventually(t, le.IsLeader, 5*time.Second, time.Millisecond, "the node never became the leader")
	}
	require.Equal(t, isLeader, le.IsLeader())
	e.producer.leaderElection = le

	bus := eventstest.NewBus()
	NewEventListener(e.producer, zap.NewNop().Sugar()).Subscribe(bus)
	return e, bus
}

func TestEventListener_OnTheLeader(t *testing.T) {
	ctx := context.Background()
	slot := time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC)

	t.Run("a created monitor is due now", func(t *testing.T) {
		mon := &monitor.Model{ID: "mon-1", Type: "http", Active: true, Interval: 60}
		e, bus := newListener(t, true, mon)

		require.NoError(t, events.Publish(ctx, bus, events.MonitorCreated, *mon))

		assert.Equal(t, map[string]float64{"mon-1": ms(t0)}, e.members(t, SchedDueKey))
		assert.Equal(t, 60, e.producer.monitorIntervals["mon-1"])
	})

	t.Run("an updated monitor is due now with its new interval", func(t *testing.T) {
		mon := &monitor.Model{ID: "mon-1", Type: "http", Active: true, Interval: 120}
		e, bus := newListener(t, true, mon)
		e.producer.monitorIntervals["mon-1"] = 60
		e.addDue(t, "mon-1", slot)

		require.NoError(t, events.Publish(ctx, bus, events.MonitorUpdated, *mon))

		assert.Equal(t, map[string]float64{"mon-1": ms(t0)}, e.members(t, SchedDueKey))
		assert.Equal(t, 120, e.producer.monitorIntervals["mon-1"])
	})

	t.Run("a monitor paused by an update leaves the schedule", func(t *testing.T) {
		mon := &monitor.Model{ID: "mon-1", Type: "http", Active: false, Interval: 60}
		e, bus := newListener(t, true, mon)
		e.producer.monitorIntervals["mon-1"] = 60
		e.addDue(t, "mon-1", slot)
		e.addLease(t, "mon-1", t0.Add(LeaseTTL))

		require.NoError(t, events.Publish(ctx, bus, events.MonitorUpdated, *mon))

		assert.Empty(t, e.members(t, SchedDueKey))
		assert.Empty(t, e.members(t, SchedLeaseKey))
		assert.NotContains(t, e.producer.monitorIntervals, "mon-1")
	})

	t.Run("a deleted monitor leaves the schedule", func(t *testing.T) {
		e, bus := newListener(t, true)
		e.producer.monitorIntervals["mon-1"] = 60
		e.addDue(t, "mon-1", slot)
		e.addLease(t, "mon-1", t0.Add(LeaseTTL))

		require.NoError(t, events.Publish(ctx, bus, events.MonitorDeleted, "mon-1"))

		assert.Empty(t, e.members(t, SchedDueKey))
		assert.Empty(t, e.members(t, SchedLeaseKey))
		assert.NotContains(t, e.producer.monitorIntervals, "mon-1")
	})
}

func TestEventListener_OnAFollower(t *testing.T) {
	slot := time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC)
	mon := &monitor.Model{ID: "mon-1", Type: "http", Active: true, Interval: 120}

	ctx := context.Background()
	for name, publish := range map[string]func(bus *eventstest.Bus) error{
		"created": func(bus *eventstest.Bus) error {
			return events.Publish(ctx, bus, events.MonitorCreated, monitor.Model{ID: "mon-new", Type: "http", Active: true, Interval: 60})
		},
		"updated": func(bus *eventstest.Bus) error { return events.Publish(ctx, bus, events.MonitorUpdated, *mon) },
		"deleted": func(bus *eventstest.Bus) error { return events.Publish(ctx, bus, events.MonitorDeleted, "mon-1") },
	} {
		t.Run("ignores a monitor "+name+" event", func(t *testing.T) {
			e, bus := newListener(t, false)
			// The leader handles the event: this node reads nothing and changes nothing
			monitorSvc := new(MockMonitorService)
			queueSvc := new(MockQueueService)
			e.producer.monitorService = monitorSvc
			e.producer.runner = NewQueueRunner(queueSvc, zap.NewNop().Sugar())
			e.producer.monitorIntervals["mon-1"] = 60
			e.addDue(t, "mon-1", slot)

			require.NoError(t, publish(bus))

			assert.Equal(t, map[string]float64{"mon-1": ms(slot)}, e.members(t, SchedDueKey))
			assert.Empty(t, e.members(t, SchedLeaseKey))
			assert.Equal(t, map[string]int{"mon-1": 60}, e.producer.monitorIntervals)
			monitorSvc.AssertNotCalled(t, "FindByID", mock.Anything, mock.Anything)
			queueSvc.AssertNotCalled(t, "GetTaskInfo", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}
