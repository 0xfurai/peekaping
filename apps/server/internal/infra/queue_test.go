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

	server, err := ProvideAsynqServer(cfg, logger)
	require.NoError(t, err)
	require.NoError(t, server.Start(mux))
	t.Cleanup(server.Shutdown)

	client, err := ProvideAsynqClient(cfg, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.Enqueue(asynq.NewTask("test:acl", []byte("client")), asynq.Queue("healthcheck"))
	require.NoError(t, err)
	require.Equal(t, "client", receive(t, processed))

	scheduler, err := ProvideAsynqScheduler(cfg, logger)
	require.NoError(t, err)
	_, err = scheduler.Register("@every 1s", asynq.NewTask("test:acl", []byte("scheduler")), asynq.Queue("healthcheck"))
	require.NoError(t, err)
	require.NoError(t, scheduler.Start())
	t.Cleanup(scheduler.Shutdown)
	require.Equal(t, "scheduler", receive(t, processed))

	inspector, err := ProvideAsynqInspector(cfg, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = inspector.Close() })

	queues, err := inspector.Queues()
	require.NoError(t, err)
	require.Contains(t, queues, "healthcheck")
}

func TestAsynqClientWithoutRedisUsernameIsRejectedByACLRedis(t *testing.T) {
	cfg := newACLRedisConfig(t, "")

	client, err := ProvideAsynqClient(cfg, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.Enqueue(asynq.NewTask("test:acl", nil))
	require.Error(t, err)
}

// A result that was enqueued already is a conflict the caller can tell from a failure: the
// probe protocol answers it duplicate, where a failure makes the probe send its batch again
func TestEnqueueUnique_ATaskTheQueueHoldsAlreadyIsATaskIDConflict(t *testing.T) {
	cfg := newACLRedisConfig(t, "peekaping")
	logger := zap.NewNop().Sugar()
	client, err := ProvideAsynqClient(cfg, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	inspector, err := ProvideAsynqInspector(cfg, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = inspector.Close() })
	service := ProvideQueueService(client, inspector, logger)
	enqueue := func(payload string) error {
		_, err := service.EnqueueUnique(context.Background(), "test:unique", payload, "unique:mon-1:1", time.Minute,
			&queue.EnqueueOptions{Queue: "ingester", Retention: time.Hour})
		return err
	}

	require.NoError(t, enqueue("first"))

	t.Run("the same unique key with another payload", func(t *testing.T) {
		assert.ErrorIs(t, enqueue("second"), queue.ErrTaskIDConflict)
	})
	t.Run("the same unique key with the same payload", func(t *testing.T) {
		assert.ErrorIs(t, enqueue("first"), queue.ErrTaskIDConflict)
	})
}

// A task's payload holds the monitor's config and the proxy's password, so a failed task logs
// its type, ID and error but never its payload. All three servers log failures the same way.
func TestAsynqServersLogAFailedTaskWithoutItsPayload(t *testing.T) {
	const payload = `{"config": "{\"basic_auth_pass\": \"secret-monitor-password\"}", "proxy_password": "secret-proxy-password"}`
	servers := []struct {
		name    string
		provide func(*config.Config, *zap.SugaredLogger) (*asynq.Server, error)
		queue   string
	}{
		{"worker", ProvideAsynqServer, "healthcheck"},
		{"ingester", ProvideIngesterAsynqServer, "ingester"},
		{"notification", ProvideNotificationAsynqServer, "notification"},
	}

	for _, tc := range servers {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newACLRedisConfig(t, "peekaping")
			core, logs := observer.New(zap.DebugLevel)
			logger := zap.New(core).Sugar()

			mux := asynq.NewServeMux()
			mux.HandleFunc("test:failing", func(context.Context, *asynq.Task) error {
				return errors.New("the monitor could not be reached")
			})
			server, err := tc.provide(cfg, logger)
			require.NoError(t, err)
			require.NoError(t, server.Start(mux))
			t.Cleanup(server.Shutdown)

			client, err := ProvideAsynqClient(cfg, logger)
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })
			_, err = client.Enqueue(asynq.NewTask("test:failing", []byte(payload)), asynq.Queue(tc.queue), asynq.TaskID("task-123"), asynq.MaxRetry(0))
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
		})
	}
}

func TestAsynqServerLogsACancelledTaskAsInfo(t *testing.T) {
	cfg := newACLRedisConfig(t, "peekaping")
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core).Sugar()

	mux := asynq.NewServeMux()
	mux.HandleFunc("test:cancelled", func(context.Context, *asynq.Task) error {
		return fmt.Errorf("check stopped: %w", context.Canceled)
	})
	server, err := ProvideAsynqServer(cfg, logger)
	require.NoError(t, err)
	require.NoError(t, server.Start(mux))
	t.Cleanup(server.Shutdown)

	client, err := ProvideAsynqClient(cfg, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	_, err = client.Enqueue(asynq.NewTask("test:cancelled", []byte("payload")), asynq.Queue("healthcheck"), asynq.TaskID("task-456"), asynq.MaxRetry(0))
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
