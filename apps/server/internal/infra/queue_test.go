package infra

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/queue"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// newACLRedisConfig starts a Redis that only accepts the ACL user "peekaping"
// and returns a config pointing at it, mirroring a shared Redis with the
// default user disabled.
func newACLRedisConfig(t *testing.T, username string) *config.Config {
	t.Helper()

	mr := miniredis.RunT(t)
	mr.RequireUserAuth("peekaping", "secret")

	host, port, err := net.SplitHostPort(mr.Addr())
	require.NoError(t, err)

	return &config.Config{
		RedisHost:        host,
		RedisPort:        port,
		RedisUsername:    username,
		RedisPassword:    "secret",
		QueueConcurrency: 1,
		Timezone:         "UTC",
	}
}

func TestAsynqProvidersAuthenticateWithRedisUsername(t *testing.T) {
	cfg := newACLRedisConfig(t, "peekaping")
	logger := zap.NewNop().Sugar()

	processed := make(chan string, 2)
	mux := asynq.NewServeMux()
	mux.HandleFunc("test:acl", func(_ context.Context, task *asynq.Task) error {
		processed <- string(task.Payload())
		return nil
	})

	server, err := ProvideNotificationAsynqServer(cfg, logger)
	require.NoError(t, err)
	require.NoError(t, server.Start(mux))
	t.Cleanup(server.Shutdown)

	client, err := ProvideAsynqClient(cfg, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.Enqueue(asynq.NewTask("test:acl", []byte("client")), asynq.Queue("notification"))
	require.NoError(t, err)
	require.Equal(t, "client", receive(t, processed))
}

func TestAsynqClientWithoutRedisUsernameIsRejectedByACLRedis(t *testing.T) {
	cfg := newACLRedisConfig(t, "")

	client, err := ProvideAsynqClient(cfg, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.Enqueue(asynq.NewTask("test:acl", nil))
	require.Error(t, err)
}

// The queue service puts a task on the queue it names, with the payload as JSON, and the retries and
// timeout its options set
func TestQueueService_EnqueueSendsTheTaskToTheNamedQueueWithItsOptions(t *testing.T) {
	cfg := newACLRedisConfig(t, "peekaping")
	logger := zap.NewNop().Sugar()
	client, err := ProvideAsynqClient(cfg, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	type taken struct {
		taskType, payload string
		maxRetry          int
		timeout           time.Duration
	}
	got := make(chan taken, 1)
	mux := asynq.NewServeMux()
	mux.HandleFunc("test:event", func(ctx context.Context, task *asynq.Task) error {
		maxRetry, _ := asynq.GetMaxRetry(ctx)
		deadline, _ := ctx.Deadline()
		got <- taken{task.Type(), string(task.Payload()), maxRetry, time.Until(deadline)}
		return nil
	})
	server, err := ProvideNotificationAsynqServer(cfg, logger)
	require.NoError(t, err)
	require.NoError(t, server.Start(mux))
	t.Cleanup(server.Shutdown)

	err = ProvideQueueService(client, logger).Enqueue(context.Background(), "test:event", map[string]string{"topic": "x"},
		&queue.EnqueueOptions{Queue: "notification", MaxRetry: 5, Timeout: time.Minute})

	require.NoError(t, err)
	select {
	case task := <-got:
		assert.Equal(t, "test:event", task.taskType)
		assert.JSONEq(t, `{"topic":"x"}`, task.payload)
		assert.Equal(t, 5, task.maxRetry)
		assert.InDelta(t, time.Minute.Seconds(), task.timeout.Seconds(), 5, "the task's timeout")
	case <-time.After(10 * time.Second):
		t.Fatal("the notification server didn't take the task")
	}
}

func TestQueueService_EnqueueRefusesAPayloadThatIsNotJSON(t *testing.T) {
	cfg := newACLRedisConfig(t, "peekaping")
	logger := zap.NewNop().Sugar()
	client, err := ProvideAsynqClient(cfg, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	err = ProvideQueueService(client, logger).Enqueue(context.Background(), "test:event", make(chan int),
		&queue.EnqueueOptions{Queue: "notification"})

	assert.ErrorContains(t, err, "failed to marshal payload")
}

func TestQueueService_EnqueueRefusesNoOptions(t *testing.T) {
	cfg := newACLRedisConfig(t, "peekaping")
	logger := zap.NewNop().Sugar()
	client, err := ProvideAsynqClient(cfg, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	err = ProvideQueueService(client, logger).Enqueue(context.Background(), "test:event", map[string]string{}, nil)

	assert.EqualError(t, err, "enqueue test:event: no queue options")
}

// A task's payload holds an event: a heartbeat with the monitor's name and the check's message. A
// failed task logs its type, ID and error but never its payload.
func TestAsynqServerLogsAFailedTaskWithoutItsPayload(t *testing.T) {
	const payload = `{"topic": "important_heartbeat", "payload": {"monitor_name": "secret-monitor-name", "msg": "secret-check-message"}}`
	cfg := newACLRedisConfig(t, "peekaping")
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core).Sugar()

	mux := asynq.NewServeMux()
	mux.HandleFunc("test:failing", func(context.Context, *asynq.Task) error {
		return errors.New("the monitor could not be reached")
	})
	server, err := ProvideNotificationAsynqServer(cfg, logger)
	require.NoError(t, err)
	require.NoError(t, server.Start(mux))
	t.Cleanup(server.Shutdown)

	client, err := ProvideAsynqClient(cfg, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	_, err = client.Enqueue(asynq.NewTask("test:failing", []byte(payload)), asynq.Queue("notification"), asynq.TaskID("task-123"), asynq.MaxRetry(0))
	require.NoError(t, err)

	require.Eventually(t, func() bool { return logs.FilterMessage("Task processing failed").Len() == 1 }, 10*time.Second, 20*time.Millisecond)

	entry := logs.FilterMessage("Task processing failed").All()[0]
	assert.Equal(t, zap.ErrorLevel, entry.Level)
	assert.Equal(t, "test:failing", entry.ContextMap()["type"])
	assert.Equal(t, "task-123", entry.ContextMap()["task_id"])
	assert.Equal(t, "the monitor could not be reached", entry.ContextMap()["error"])
	for _, logged := range logs.All() {
		assert.NotContains(t, logged.Message, "secret-")
		for key, value := range logged.ContextMap() {
			assert.NotEqual(t, "payload", key)
			assert.NotContains(t, fmt.Sprint(value), "secret-")
		}
	}
}

func TestAsynqServerLogsACancelledTaskAsInfo(t *testing.T) {
	cfg := newACLRedisConfig(t, "peekaping")
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core).Sugar()

	mux := asynq.NewServeMux()
	mux.HandleFunc("test:cancelled", func(context.Context, *asynq.Task) error {
		return fmt.Errorf("task stopped: %w", context.Canceled)
	})
	server, err := ProvideNotificationAsynqServer(cfg, logger)
	require.NoError(t, err)
	require.NoError(t, server.Start(mux))
	t.Cleanup(server.Shutdown)

	client, err := ProvideAsynqClient(cfg, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	_, err = client.Enqueue(asynq.NewTask("test:cancelled", []byte("payload")), asynq.Queue("notification"), asynq.TaskID("task-456"), asynq.MaxRetry(0))
	require.NoError(t, err)

	require.Eventually(t, func() bool { return logs.FilterMessage("Task cancelled").Len() == 1 }, 10*time.Second, 20*time.Millisecond)

	entry := logs.FilterMessage("Task cancelled").All()[0]
	assert.Equal(t, zap.InfoLevel, entry.Level)
	assert.Equal(t, "test:cancelled", entry.ContextMap()["type"])
	assert.Equal(t, "task-456", entry.ContextMap()["task_id"])
	assert.Zero(t, logs.FilterMessage("Task processing failed").Len(), "a cancelled task is not a failure")
}

func receive(t *testing.T, ch <-chan string) string {
	t.Helper()

	select {
	case payload := <-ch:
		return payload
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the task to be processed")
		return ""
	}
}
