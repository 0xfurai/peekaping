package notification_channel

import (
	"encoding/json"
	"testing"

	"peekaping/internal/reporting/reportingtest"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// A consumer with no bus panics on the first task it takes, as a bug in handing over an event
// would. The api's asynq server recovers it, and reports it when an admin opted in.
func TestQueueConsumer_ReportsAPanicInANotificationTask(t *testing.T) {
	recorder := reportingtest.Install(t)
	server, client := reportingtest.NewAsynq(t)
	consumer := NewQueueConsumer(server, nil, zap.NewNop().Sugar())
	require.NoError(t, consumer.Start())
	t.Cleanup(consumer.Stop)
	payload, err := json.Marshal(queuedEvent{Topic: "heartbeat.important", Payload: json.RawMessage(`{}`)})
	require.NoError(t, err)

	_, err = client.Enqueue(asynq.NewTask(TaskTypeNotification, payload), asynq.MaxRetry(0))
	require.NoError(t, err)

	events := recorder.Wait(t, 1)
	assert.Equal(t, "asynq", events[0].Tags["component"])
}
