package ingester

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"peekaping/internal/reporting/reportingtest"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// blockingService holds a result, ignoring its context, until released
type blockingService struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingService) Ingest(context.Context, *Result) error {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return nil
}

// Asynq gives up on a task still running after its ShutdownTimeout: it puts the task back in
// the queue, cancels its context and returns, without waiting for the handler. Stop waits for
// the handler, which may still be storing the heartbeat.
func TestStop_WaitsForTheHandlerOfATaskAsynqGaveUpOn(t *testing.T) {
	redis := miniredis.RunT(t)
	service := &blockingService{entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(service.release) }) }
	t.Cleanup(release)
	redisOpt := asynq.RedisClientOpt{Addr: redis.Addr()}
	server := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency:     1,
		Queues:          map[string]int{"ingester": 1},
		ShutdownTimeout: 200 * time.Millisecond,
		LogLevel:        asynq.ErrorLevel,
	})
	logger := zap.NewNop().Sugar()
	ing := NewIngester(server, NewIngesterTaskHandler(service, logger), logger)
	require.NoError(t, ing.Start())
	client := asynq.NewClient(redisOpt)
	t.Cleanup(func() { _ = client.Close() })
	payload, err := json.Marshal(Result{MonitorID: monitorID, Status: up})
	require.NoError(t, err)
	_, err = client.Enqueue(asynq.NewTask(TaskTypeIngester, payload), asynq.Queue("ingester"))
	require.NoError(t, err)
	select {
	case <-service.entered:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the ingester didn't take the task")
	}

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ing.Stop()
	}()

	// Long enough for asynq to give up on the task and finish its own shutdown
	select {
	case <-stopped:
		require.FailNow(t, "Stop returned while the handler was still running")
	case <-time.After(3 * time.Second):
	}
	release()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "Stop didn't return once the handler did")
	}
}

// panickingService panics when it stores a result, as a bug in the ingest would
type panickingService struct{}

func (panickingService) Ingest(context.Context, *Result) error { panic("ingest failed") }

func TestIngester_ReportsAPanicInAnIngestTask(t *testing.T) {
	recorder := reportingtest.Install(t)
	server, client := reportingtest.NewAsynq(t)
	logger := zap.NewNop().Sugar()
	ing := NewIngester(server, NewIngesterTaskHandler(panickingService{}, logger), logger)
	require.NoError(t, ing.Start())
	t.Cleanup(ing.Stop)
	payload, err := json.Marshal(Result{MonitorID: "mon-1"})
	require.NoError(t, err)

	_, err = client.Enqueue(asynq.NewTask(TaskTypeIngester, payload), asynq.MaxRetry(0))
	require.NoError(t, err)

	events := recorder.Wait(t, 1)
	assert.Equal(t, "asynq", events[0].Tags["component"])
	assert.Equal(t, "string", events[0].Exception[0].Type)
}
