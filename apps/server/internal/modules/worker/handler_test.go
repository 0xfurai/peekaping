package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"peekaping/internal/modules/certificate"
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

// fakeQueue records results sent to the ingester; ProcessTask uses no other method
type fakeQueue struct {
	queue.Service
	ingested []IngesterTaskPayload
}

func (q *fakeQueue) EnqueueUnique(_ context.Context, _ string, payload interface{}, _ string, _ time.Duration, _ *queue.EnqueueOptions) (*queue.TaskInfo, error) {
	q.ingested = append(q.ingested, payload.(IngesterTaskPayload))
	return &queue.TaskInfo{}, nil
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
	q := &fakeQueue{}

	require.NoError(t, newTestHandler(q).ProcessTask(context.Background(), asynq.NewTask(TaskTypeHealthCheck, task)))

	require.Len(t, q.ingested, 1)
	got := q.ingested[0]
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

	require.NoError(t, newTestHandler(q).ProcessTask(context.Background(), asynq.NewTask(TaskTypeHealthCheck, task)))

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

// The scheduler ingests an ingester.Result and the worker enqueues an IngesterTaskPayload, and the
// ingester decodes the task into a Result. They have to encode alike.
func TestIngesterTaskPayload_EncodesLikeTheIngestersResult(t *testing.T) {
	start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	result := ingester.Result{
		MonitorID:          "mon-1",
		MonitorName:        "Test",
		MonitorType:        "http",
		MonitorInterval:    60,
		MonitorTimeout:     48,
		MonitorMaxRetries:  3,
		MonitorRetryInt:    30,
		MonitorResendInt:   5,
		MonitorConfig:      `{"url":"https://example.com"}`,
		Status:             shared.MonitorStatusDown,
		Message:            "503 - Service Unavailable",
		PingMs:             12,
		StartTime:          start,
		EndTime:            start.Add(12 * time.Millisecond),
		IsUnderMaintenance: true,
		TLSInfo:            &certificate.TLSInfo{Valid: true},
		CheckCertExpiry:    true,
	}

	fromResult, err := json.Marshal(result)
	require.NoError(t, err)
	fromPayload, err := json.Marshal(IngesterTaskPayload(result))
	require.NoError(t, err)

	assert.JSONEq(t, string(fromResult), string(fromPayload))
}
