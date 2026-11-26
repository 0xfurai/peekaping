package notification_channel

import (
	"context"
	"testing"

	"peekaping/internal/modules/events"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// A task whose event the api's bus refuses must fail, so that asynq tries it again on a replica
// that can take it
func TestQueueConsumer_FailsATaskWhenTheEventBusIsClosed(t *testing.T) {
	bus := events.NewMemoryBus()
	require.NoError(t, bus.Close(context.Background()))
	consumer := NewQueueConsumer(nil, NewQueuedBus(bus, nil), zap.NewNop().Sugar())
	task := asynq.NewTask(TaskTypeNotification, []byte(`{"topic":"important.heartbeat","payload":{"monitor_id":"mon-1"}}`))

	err := consumer.ProcessTask(context.Background(), task)

	require.ErrorIs(t, err, events.ErrClosed)
	assert.NotErrorIs(t, err, asynq.SkipRetry)
}

// A payload that makes no sense fails the same way on every try, so asynq must not retry it
func TestQueueConsumer_DoesNotRetryATaskItCannotRead(t *testing.T) {
	for name, payload := range map[string]string{
		"not JSON":           `important.heartbeat`,
		"no topic":           `{"payload":{"monitor_id":"mon-1"}}`,
		"no event":           `{"topic":"important.heartbeat"}`,
		"an unwrapped event": `{"monitor_id":"mon-1"}`,
	} {
		t.Run(name, func(t *testing.T) {
			consumer := NewQueueConsumer(nil, NewQueuedBus(events.NewMemoryBus(), nil), zap.NewNop().Sugar())

			err := consumer.ProcessTask(context.Background(), asynq.NewTask(TaskTypeNotification, []byte(payload)))

			assert.ErrorIs(t, err, asynq.SkipRetry)
		})
	}
}

func TestQueuedBus_RefusesANotificationEventOnceClosed(t *testing.T) {
	bus := NewQueuedBus(events.NewMemoryBus(), nil)
	require.NoError(t, bus.Close(context.Background()))

	err := events.Publish(context.Background(), bus, events.ImportantHeartbeat, *downHeartbeat("mon-1"))

	assert.ErrorIs(t, err, events.ErrClosed)
}

// Redis pub/sub hands an event to every api replica, so a topic the listener subscribes to and
// the queue doesn't carry would be sent once by each replica
func TestQueuedTopics_AreEveryTopicTheNotificationListenerSubscribesTo(t *testing.T) {
	wireNames := []string{"important.heartbeat", "certificate.expiry", "monitor.stale", "monitor.stale.recovered", "probe.offline", "probe.online"}
	f := newListenerFixture(t)

	assert.ElementsMatch(t, wireNames, f.bus.SubscribedTopics(), "the topics the listener subscribes to")
	assert.ElementsMatch(t, wireNames, queuedTopics, "the topics QueuedBus sends through the queue")
}
