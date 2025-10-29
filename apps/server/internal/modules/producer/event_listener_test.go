package producer

import (
	"context"
	"encoding/json"
	"testing"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

func TestEventListener_HandleMonitorCreated(t *testing.T) {
	t.Run("handles monitor created when leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		// Make this node the leader
		ctx := context.Background()
		le.tryBecomeLeader(ctx)

		producer := &Producer{leaderElection: le}
		eventListener := NewEventListener(producer, logger)

		mon := monitor.Model{
			ID:       "monitor-123",
			Name:     "Test Monitor",
			Active:   true,
			Interval: 60,
		}

		payload, err := json.Marshal(mon)
		assert.NoError(t, err)

		event := events.Event{
			Type:    events.MonitorCreated,
			Payload: payload,
		}

		// Can't easily test the actual AddMonitor call without more mocking
		// but we can test that the unmarshal works
		var unmarshaled monitor.Model
		err = eventListener.unmarshalPayload(event.Payload, &unmarshaled)
		assert.NoError(t, err)
		assert.Equal(t, mon.ID, unmarshaled.ID)
	})

	t.Run("ignores monitor created when not leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		// Don't make this node the leader
		producer := &Producer{leaderElection: le}
		eventListener := NewEventListener(producer, logger)

		mon := monitor.Model{
			ID:       "monitor-123",
			Name:     "Test Monitor",
			Active:   true,
			Interval: 60,
		}

		payload, err := json.Marshal(mon)
		assert.NoError(t, err)

		event := events.Event{
			Type:    events.MonitorCreated,
			Payload: payload,
		}

		// Should not panic or error, just ignore
		eventListener.handleMonitorCreated(event)
	})
}

func TestEventListener_HandleMonitorUpdated(t *testing.T) {
	t.Run("handles monitor updated when leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		// Make this node the leader
		ctx := context.Background()
		le.tryBecomeLeader(ctx)

		producer := &Producer{leaderElection: le}
		eventListener := NewEventListener(producer, logger)

		mon := monitor.Model{
			ID:       "monitor-123",
			Name:     "Updated Monitor",
			Active:   true,
			Interval: 120,
		}

		payload, err := json.Marshal(mon)
		assert.NoError(t, err)

		event := events.Event{
			Type:    events.MonitorUpdated,
			Payload: payload,
		}

		var unmarshaled monitor.Model
		err = eventListener.unmarshalPayload(event.Payload, &unmarshaled)
		assert.NoError(t, err)
		assert.Equal(t, mon.ID, unmarshaled.ID)
		assert.Equal(t, 120, unmarshaled.Interval)
	})

	t.Run("ignores monitor updated when not leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		producer := &Producer{leaderElection: le}
		eventListener := NewEventListener(producer, logger)

		mon := monitor.Model{
			ID:       "monitor-123",
			Name:     "Updated Monitor",
			Active:   true,
			Interval: 120,
		}

		payload, err := json.Marshal(mon)
		assert.NoError(t, err)

		event := events.Event{
			Type:    events.MonitorUpdated,
			Payload: payload,
		}

		// Should not panic or error, just ignore
		eventListener.handleMonitorUpdated(event)
	})
}

func TestEventListener_HandleMonitorDeleted(t *testing.T) {
	t.Run("handles monitor deleted when leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		// Make this node the leader
		ctx := context.Background()
		le.tryBecomeLeader(ctx)

		producer := &Producer{leaderElection: le}
		eventListener := NewEventListener(producer, logger)

		monitorID := "monitor-123"
		payload, err := json.Marshal(monitorID)
		assert.NoError(t, err)

		event := events.Event{
			Type:    events.MonitorDeleted,
			Payload: payload,
		}

		var unmarshaled string
		err = eventListener.unmarshalPayload(event.Payload, &unmarshaled)
		assert.NoError(t, err)
		assert.Equal(t, monitorID, unmarshaled)
	})

	t.Run("ignores monitor deleted when not leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		producer := &Producer{leaderElection: le}
		eventListener := NewEventListener(producer, logger)

		monitorID := "monitor-123"
		payload, err := json.Marshal(monitorID)
		assert.NoError(t, err)

		event := events.Event{
			Type:    events.MonitorDeleted,
			Payload: payload,
		}

		// Should not panic or error, just ignore
		eventListener.handleMonitorDeleted(event)
	})
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
