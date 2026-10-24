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
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/shared"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeIngester records the results sent to the ingester
type fakeIngester struct {
	ingested []*ingester.Result
}

func (f *fakeIngester) Ingest(_ context.Context, r *ingester.Result) error {
	f.ingested = append(f.ingested, r)
	return nil
}

// enqueued is one unique enqueue, with the payload as the queue writes it
type enqueued struct {
	taskType  string
	payload   []byte
	uniqueKey string
	ttl       time.Duration
	opts      *queue.EnqueueOptions
}

// recordingQueue records every unique enqueue
type recordingQueue struct {
	queue.Service
	enqueued []enqueued
}

func (q *recordingQueue) EnqueueUnique(_ context.Context, taskType string, payload interface{}, uniqueKey string, ttl time.Duration, opts *queue.EnqueueOptions) (*queue.TaskInfo, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	q.enqueued = append(q.enqueued, enqueued{taskType, encoded, uniqueKey, ttl, opts})
	return &queue.TaskInfo{}, nil
}

func newTestHandler(ingesterService ingester.Service) *HealthCheckTaskHandler {
	logger := zap.NewNop().Sugar()
	registry := executor.NewExecutorRegistry(logger)
	return NewHealthCheckTaskHandler(registry, healthcheck.NewHealthCheck(nil, registry, logger), ingesterService, logger)
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
		results := &fakeIngester{}

		err := newTestHandler(results).ProcessTask(context.Background(), httpCheckTask(t, hanging.URL, 1))
		require.NoError(t, err)
		require.Len(t, results.ingested, 1)
		assert.Equal(t, shared.MonitorStatusDown, results.ingested[0].Status)
	})

	t.Run("drops the result of a check cancelled by a worker shutdown", func(t *testing.T) {
		results := &fakeIngester{}
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(100*time.Millisecond, cancel)

		err := newTestHandler(results).ProcessTask(ctx, httpCheckTask(t, hanging.URL, 5))
		assert.ErrorIs(t, err, context.Canceled)
		assert.Empty(t, results.ingested)
	})
}

func TestProcessTask_SendsTheResultWithTheMonitorSettingsToTheIngester(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer target.Close()
	task, err := json.Marshal(HealthCheckTaskPayload{
		MonitorID:       "mon-1",
		MonitorName:     "Test",
		MonitorType:     "http",
		Interval:        60,
		Timeout:         5,
		MaxRetries:      3,
		RetryInterval:   30,
		ResendInterval:  5,
		Config:          fmt.Sprintf(`{"url": %q, "method": "GET", "encoding": "json", "authMethod": "none", "accepted_statuscodes": ["2XX"]}`, target.URL),
		ScheduledAt:     time.Now().UTC(),
		CheckCertExpiry: true,
	})
	require.NoError(t, err)
	results := &fakeIngester{}

	require.NoError(t, newTestHandler(results).ProcessTask(context.Background(), asynq.NewTask(TaskTypeHealthCheck, task)))

	require.Len(t, results.ingested, 1)
	got := results.ingested[0]
	assert.Equal(t, "mon-1", got.MonitorID)
	assert.Equal(t, "Test", got.MonitorName)
	assert.Equal(t, "http", got.MonitorType)
	assert.Equal(t, 60, got.MonitorInterval)
	assert.Equal(t, 5, got.MonitorTimeout)
	assert.Equal(t, 3, got.MonitorMaxRetries)
	assert.Equal(t, 30, got.MonitorRetryInt)
	assert.Equal(t, 5, got.MonitorResendInt)
	assert.Contains(t, got.MonitorConfig, target.URL)
	assert.Equal(t, shared.MonitorStatusUp, got.Status)
	assert.False(t, got.StartTime.IsZero())
	assert.False(t, got.EndTime.Before(got.StartTime))
	assert.False(t, got.IsUnderMaintenance)
	assert.True(t, got.CheckCertExpiry)
}

// Cluster mode's ingester reads a check's result as a monitor:ingest task. The task, its options
// and its payload are pinned so that the ingester keeps reading them the same way.
func TestProcessTask_EnqueuesTheIngesterTaskClusterModeReads(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer target.Close()
	config := fmt.Sprintf(`{"url": %q, "method": "GET", "encoding": "json", "authMethod": "none", "accepted_statuscodes": ["2XX"]}`, target.URL)
	task, err := json.Marshal(HealthCheckTaskPayload{
		MonitorID:       "mon-1",
		MonitorName:     "Test",
		MonitorType:     "http",
		Interval:        60,
		Timeout:         5,
		MaxRetries:      3,
		RetryInterval:   30,
		ResendInterval:  5,
		Config:          config,
		ScheduledAt:     time.Now().UTC(),
		CheckCertExpiry: true,
	})
	require.NoError(t, err)
	q := &recordingQueue{}

	require.NoError(t, newTestHandler(ingester.NewQueueService(q)).ProcessTask(context.Background(), asynq.NewTask(TaskTypeHealthCheck, task)))

	require.Len(t, q.enqueued, 1)
	got := q.enqueued[0]
	assert.Equal(t, "monitor:ingest", got.taskType)
	assert.Equal(t, &queue.EnqueueOptions{Queue: "ingester", MaxRetry: 3, Timeout: 2 * time.Minute, Retention: time.Hour}, got.opts)
	assert.Equal(t, time.Second, got.ttl)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(got.payload, &payload))
	startedAt, err := time.Parse(time.RFC3339Nano, payload["start_time"].(string))
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("ingest:mon-1:%d", startedAt.UnixNano()), got.uniqueKey)
	// The times and the response time are the check's own
	assert.IsType(t, "", payload["end_time"])
	assert.IsType(t, float64(0), payload["ping_ms"])
	payload["start_time"], payload["end_time"], payload["ping_ms"] = "started at", "ended at", "ping"
	pinned, err := json.Marshal(payload)
	require.NoError(t, err)
	assert.JSONEq(t, fmt.Sprintf(`{
		"probe_id": "local",
		"monitor_id": "mon-1",
		"monitor_name": "Test",
		"monitor_type": "http",
		"monitor_interval": 60,
		"monitor_timeout": 5,
		"monitor_max_retries": 3,
		"monitor_retry_interval": 30,
		"monitor_resend_interval": 5,
		"monitor_config": %q,
		"status": 1,
		"message": "200 - 200 OK",
		"ping_ms": "ping",
		"start_time": "started at",
		"end_time": "ended at",
		"is_under_maintenance": false,
		"check_cert_expiry": true
	}`, config), string(pinned))
}
