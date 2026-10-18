package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/shared"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeQueue records results sent to the ingester; ProcessTask uses no other method
type fakeQueue struct {
	queue.Service
	ingested []IngesterTaskPayload
}

func (q *fakeQueue) EnqueueUnique(_ context.Context, _ string, payload interface{}, _ string, _ time.Duration, _ *queue.EnqueueOptions) (*queue.TaskInfo, error) {
	q.ingested = append(q.ingested, payload.(IngesterTaskPayload))
	return &queue.TaskInfo{}, nil
}

func newTestHandler(q queue.Service) *HealthCheckTaskHandler {
	logger := zap.NewNop().Sugar()
	registry := executor.NewExecutorRegistry(logger)
	return NewHealthCheckTaskHandler(registry, healthcheck.NewHealthCheck(nil, registry, logger), q, logger)
}

func httpCheckTask(t *testing.T, url string, timeout int) *asynq.Task {
	payload, err := json.Marshal(HealthCheckTaskPayload{
		MonitorID:   "mon-1",
		MonitorName: "Test",
		MonitorType: "http",
		Interval:    60,
		Timeout:     timeout,
		Config:      fmt.Sprintf(`{"url": %q, "method": "GET", "encoding": "json", "authMethod": "none", "accepted_statuscodes": ["2XX"]}`, url),
		ScheduledAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	return asynq.NewTask(TaskTypeHealthCheck, payload)
}

func TestProcessTask(t *testing.T) {
	hanging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer hanging.Close()

	t.Run("reports a timed-out check as DOWN without failing the task", func(t *testing.T) {
		q := &fakeQueue{}

		err := newTestHandler(q).ProcessTask(context.Background(), httpCheckTask(t, hanging.URL, 1))
		require.NoError(t, err)
		require.Len(t, q.ingested, 1)
		assert.Equal(t, shared.MonitorStatusDown, q.ingested[0].Status)
	})

	t.Run("drops the result of a check cancelled by a worker shutdown", func(t *testing.T) {
		q := &fakeQueue{}
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(100*time.Millisecond, cancel)

		err := newTestHandler(q).ProcessTask(ctx, httpCheckTask(t, hanging.URL, 5))
		assert.ErrorIs(t, err, context.Canceled)
		assert.Empty(t, q.ingested)
	})
}
