package producer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"peekaping/internal/modules/events"
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
	le := NewLeaderElection(client, "node1", logger)
	producer := &Producer{leaderElection: le}

	eventListener := NewEventListener(producer, logger)

	assert.NotNil(t, eventListener)
	assert.Equal(t, producer, eventListener.producer)
}

func TestEventListener_Subscribe(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()

	logger := zap.NewNop().Sugar()
	le := NewLeaderElection(client, "node1", logger)
	producer := &Producer{leaderElection: le}

	eventListener := NewEventListener(producer, logger)
	eventBus := NewMockEventBus()

	// Mock Subscribe calls - use Anything instead of AnythingOfType
	eventBus.On("Subscribe", events.MonitorCreated, mock.Anything).Return()
	eventBus.On("Subscribe", events.MonitorUpdated, mock.Anything).Return()
	eventBus.On("Subscribe", events.MonitorDeleted, mock.Anything).Return()

	eventListener.Subscribe(eventBus)

	// Verify Subscribe was called 3 times
	eventBus.AssertNumberOfCalls(t, "Subscribe", 3)
}

// newListener subscribes an event listener on a producer that is the leader,
// or a follower of node2, and returns the bus it listens on.
func newListener(t *testing.T, leader bool, monitors ...*monitor.Model) (*engine, *MockEventBus) {
	t.Helper()
	ctx := context.Background()
	e := newEngine(t, monitors...)
	if !leader {
		require.NoError(t, e.rdb.Set(ctx, LeaderKey, "node2", LeaderTTL).Err())
	}
	le := NewLeaderElection(e.rdb, "node1", zap.NewNop().Sugar())
	le.tryBecomeLeader(ctx)
	require.Equal(t, leader, le.IsLeader())
	e.producer.leaderElection = le

	bus := NewMockEventBus()
	bus.On("Subscribe", mock.Anything, mock.Anything).Return()
	NewEventListener(e.producer, zap.NewNop().Sugar()).Subscribe(bus)
	return e, bus
}

func TestEventListener_OnTheLeader(t *testing.T) {
	slot := time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC)

	t.Run("a created monitor is due now", func(t *testing.T) {
		mon := &monitor.Model{ID: "mon-1", Type: "http", Active: true, Interval: 60}
		e, bus := newListener(t, true, mon)

		bus.TriggerEvent(events.MonitorCreated, events.Event{Type: events.MonitorCreated, Payload: mon})

		assert.Equal(t, map[string]float64{"mon-1": ms(t0)}, e.members(t, SchedDueKey))
		assert.Equal(t, 60, e.producer.monitorIntervals["mon-1"])
	})

	t.Run("an updated monitor is due now with its new interval", func(t *testing.T) {
		mon := &monitor.Model{ID: "mon-1", Type: "http", Active: true, Interval: 120}
		e, bus := newListener(t, true, mon)
		e.producer.monitorIntervals["mon-1"] = 60
		e.addDue(t, "mon-1", slot)

		bus.TriggerEvent(events.MonitorUpdated, events.Event{Type: events.MonitorUpdated, Payload: mon})

		assert.Equal(t, map[string]float64{"mon-1": ms(t0)}, e.members(t, SchedDueKey))
		assert.Equal(t, 120, e.producer.monitorIntervals["mon-1"])
	})

	t.Run("a monitor paused by an update leaves the schedule", func(t *testing.T) {
		mon := &monitor.Model{ID: "mon-1", Type: "http", Active: false, Interval: 60}
		e, bus := newListener(t, true, mon)
		e.producer.monitorIntervals["mon-1"] = 60
		e.addDue(t, "mon-1", slot)
		e.addLease(t, "mon-1", t0.Add(LeaseTTL))

		bus.TriggerEvent(events.MonitorUpdated, events.Event{Type: events.MonitorUpdated, Payload: mon})

		assert.Empty(t, e.members(t, SchedDueKey))
		assert.Empty(t, e.members(t, SchedLeaseKey))
		assert.NotContains(t, e.producer.monitorIntervals, "mon-1")
	})

	t.Run("a deleted monitor leaves the schedule", func(t *testing.T) {
		e, bus := newListener(t, true)
		e.producer.monitorIntervals["mon-1"] = 60
		e.addDue(t, "mon-1", slot)
		e.addLease(t, "mon-1", t0.Add(LeaseTTL))

		bus.TriggerEvent(events.MonitorDeleted, events.Event{Type: events.MonitorDeleted, Payload: "mon-1"})

		assert.Empty(t, e.members(t, SchedDueKey))
		assert.Empty(t, e.members(t, SchedLeaseKey))
		assert.NotContains(t, e.producer.monitorIntervals, "mon-1")
	})
}

func TestEventListener_OnAFollower(t *testing.T) {
	slot := time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC)
	mon := &monitor.Model{ID: "mon-1", Type: "http", Active: true, Interval: 120}

	for name, event := range map[string]events.Event{
		"created": {Type: events.MonitorCreated, Payload: &monitor.Model{ID: "mon-new", Type: "http", Active: true, Interval: 60}},
		"updated": {Type: events.MonitorUpdated, Payload: mon},
		"deleted": {Type: events.MonitorDeleted, Payload: "mon-1"},
	} {
		t.Run("ignores a monitor "+name+" event", func(t *testing.T) {
			e, bus := newListener(t, false)
			// The leader handles the event: this node reads nothing and changes nothing
			monitorSvc := new(MockMonitorService)
			queueSvc := new(MockQueueService)
			e.producer.monitorService = monitorSvc
			e.producer.queueService = queueSvc
			e.producer.monitorIntervals["mon-1"] = 60
			e.addDue(t, "mon-1", slot)

			bus.TriggerEvent(event.Type, event)

			assert.Equal(t, map[string]float64{"mon-1": ms(slot)}, e.members(t, SchedDueKey))
			assert.Empty(t, e.members(t, SchedLeaseKey))
			assert.Equal(t, map[string]int{"mon-1": 60}, e.producer.monitorIntervals)
			monitorSvc.AssertNotCalled(t, "FindByID", mock.Anything, mock.Anything)
			queueSvc.AssertNotCalled(t, "GetTaskInfo", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func TestEventListener_UnmarshalPayload(t *testing.T) {
	logger := zap.NewNop().Sugar()
	eventListener := &EventListener{logger: logger}

	t.Run("unmarshal json.RawMessage", func(t *testing.T) {
		data := map[string]string{"id": "test-123", "name": "Test"}
		jsonData, err := json.Marshal(data)
		assert.NoError(t, err)

		payload := json.RawMessage(jsonData)

		var result map[string]string
		err = eventListener.unmarshalPayload(payload, &result)
		assert.NoError(t, err)
		assert.Equal(t, "test-123", result["id"])
		assert.Equal(t, "Test", result["name"])
	})

	t.Run("unmarshal byte slice", func(t *testing.T) {
		data := map[string]string{"id": "test-456", "name": "Test2"}
		jsonData, err := json.Marshal(data)
		assert.NoError(t, err)

		var result map[string]string
		err = eventListener.unmarshalPayload(jsonData, &result)
		assert.NoError(t, err)
		assert.Equal(t, "test-456", result["id"])
		assert.Equal(t, "Test2", result["name"])
	})

	t.Run("unmarshal Go object", func(t *testing.T) {
		data := map[string]string{"id": "test-789", "name": "Test3"}

		var result map[string]string
		err := eventListener.unmarshalPayload(data, &result)
		assert.NoError(t, err)
		assert.Equal(t, "test-789", result["id"])
		assert.Equal(t, "Test3", result["name"])
	})

	t.Run("unmarshal simple string", func(t *testing.T) {
		payload := "simple-string-id"

		var result string
		err := eventListener.unmarshalPayload(payload, &result)
		assert.NoError(t, err)
		assert.Equal(t, "simple-string-id", result)
	})

	t.Run("unmarshal error on invalid JSON", func(t *testing.T) {
		payload := json.RawMessage([]byte("invalid json"))

		var result map[string]string
		err := eventListener.unmarshalPayload(payload, &result)
		assert.Error(t, err)
	})
}
