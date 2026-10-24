package healthcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestParsePushStatus(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  shared.MonitorStatus
	}{
		{name: "up string", value: "up", want: shared.MonitorStatusUp},
		{name: "down string", value: "down", want: shared.MonitorStatusDown},
		{name: "case and whitespace insensitive", value: " UP ", want: shared.MonitorStatusUp},
		{name: "numeric up", value: "1", want: shared.MonitorStatusUp},
		{name: "numeric down", value: "0", want: shared.MonitorStatusDown},
		{name: "missing defaults to up", value: "", want: shared.MonitorStatusUp},
		{name: "failure word is down", value: "fail", want: shared.MonitorStatusDown},
		{name: "typo is down", value: "dwon", want: shared.MonitorStatusDown},
		{name: "maintenance number is down", value: "3", want: shared.MonitorStatusDown},
		{name: "out of range number is down", value: "-1", want: shared.MonitorStatusDown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePushStatus(tt.value); got != tt.want {
				t.Errorf("parsePushStatus(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestParsePushPing(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		want   int
		wantOK bool
	}{
		{name: "integer", value: "500", want: 500, wantOK: true},
		{name: "decimal is rounded", value: "123.5", want: 124, wantOK: true},
		{name: "whitespace is trimmed", value: " 42 ", want: 42, wantOK: true},
		{name: "missing is zero", value: "", want: 0, wantOK: true},
		{name: "largest column value", value: "2147483647", want: 2147483647, wantOK: true},
		{name: "malformed is dropped", value: "unknown", want: 0, wantOK: false},
		{name: "negative is dropped", value: "-500", want: 0, wantOK: false},
		{name: "beyond the column is dropped", value: "3000000000", want: 0, wantOK: false},
		{name: "NaN is dropped", value: "NaN", want: 0, wantOK: false},
		{name: "infinity is dropped", value: "Inf", want: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parsePushPing(tt.value)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("parsePushPing(%q) = (%d, %t), want (%d, %t)", tt.value, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// pushMonitorService finds one active monitor by its push token.
type pushMonitorService struct {
	monitor.Service
	monitor *monitor.Model
}

func (s *pushMonitorService) FindOneByPushToken(_ context.Context, token string) (*monitor.Model, error) {
	if token != s.monitor.PushToken {
		return nil, nil
	}
	return s.monitor, nil
}

// capturingIngester records every result it is asked to ingest.
type capturingIngester struct {
	results []*ingester.Result
}

func (c *capturingIngester) Ingest(_ context.Context, r *ingester.Result) error {
	c.results = append(c.results, r)
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

// noMaintenance is a maintenance service with no windows, so a pushed
// heartbeat keeps the status it reports.
type noMaintenance struct{ maintenance.Service }

func (noMaintenance) GetMaintenancesByMonitorID(context.Context, string) ([]*maintenance.Model, error) {
	return nil, nil
}

func TestPushEndpoint(t *testing.T) {

	push := func(t *testing.T, query string) *ingester.Result {
		t.Helper()
		monitors := &pushMonitorService{monitor: &monitor.Model{ID: "mon-1", Type: "push", Active: true, PushToken: "token"}}
		results := &capturingIngester{}
		router := httpx.NewRouter()
		RegisterPushEndpoint(router.Group("/api/v1"), monitors, nil, results, noMaintenance{}, zap.NewNop().Sugar())

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/push/token?"+query, nil))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Len(t, results.results, 1)
		return results.results[0]
	}

	t.Run("URL copied from the UI records an up beat", func(t *testing.T) {
		payload := push(t, "status=up&msg=OK&ping=")
		assert.Equal(t, shared.MonitorStatusUp, payload.Status)
		assert.Equal(t, "OK", payload.Message)
		assert.Equal(t, 0, payload.PingMs)
	})

	t.Run("forwards status, message and ping", func(t *testing.T) {
		payload := push(t, "status=down&msg=disk+full&ping=12.4")
		assert.Equal(t, shared.MonitorStatusDown, payload.Status)
		assert.Equal(t, "disk full", payload.Message)
		assert.Equal(t, 12, payload.PingMs)
	})

	t.Run("down beat without a message is not reported as OK", func(t *testing.T) {
		payload := push(t, "status=down")
		assert.Equal(t, "Down", payload.Message)
	})

	t.Run("invalid ping keeps the beat", func(t *testing.T) {
		payload := push(t, "status=up&ping=-5")
		assert.Equal(t, shared.MonitorStatusUp, payload.Status)
		assert.Equal(t, 0, payload.PingMs)
	})
}

// Cluster mode's ingester reads a push as a monitor:ingest task. The task, its options and its
// payload are pinned so that the ingester keeps reading them the same way.
func TestPushEndpoint_EnqueuesTheIngesterTaskClusterModeReads(t *testing.T) {
	monitors := &pushMonitorService{monitor: &monitor.Model{
		ID:             "mon-1",
		Name:           "Nightly backup",
		Type:           "push",
		Interval:       60,
		Timeout:        16,
		MaxRetries:     2,
		RetryInterval:  30,
		ResendInterval: 5,
		Config:         `{"pushToken":"token"}`,
		Active:         true,
		PushToken:      "token",
	}}
	q := &recordingQueue{}
	router := httpx.NewRouter()
	RegisterPushEndpoint(router.Group("/api/v1"), monitors, nil, ingester.NewPushQueueService(q), noMaintenance{}, zap.NewNop().Sugar())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/push/token?status=down&msg=disk+full&ping=12", nil))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, q.enqueued, 1)
	got := q.enqueued[0]
	assert.Equal(t, "monitor:ingest", got.taskType)
	assert.Equal(t, &queue.EnqueueOptions{Queue: "ingester", MaxRetry: 3, Timeout: 2 * time.Minute, Retention: time.Hour}, got.opts)
	assert.Equal(t, 5*time.Minute, got.ttl)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(got.payload, &payload))
	pushedAt, err := time.Parse(time.RFC3339Nano, payload["start_time"].(string))
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("ingest:push:mon-1:local:%d", pushedAt.UnixNano()), got.uniqueKey)
	assert.Equal(t, payload["start_time"], payload["end_time"])
	payload["start_time"], payload["end_time"] = "pushed at", "pushed at"
	pinned, err := json.Marshal(payload)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"probe_id": "local",
		"monitor_id": "mon-1",
		"monitor_name": "Nightly backup",
		"monitor_type": "push",
		"monitor_interval": 60,
		"monitor_timeout": 16,
		"monitor_max_retries": 2,
		"monitor_retry_interval": 30,
		"monitor_resend_interval": 5,
		"monitor_config": "{\"pushToken\":\"token\"}",
		"status": 0,
		"message": "disk full",
		"ping_ms": 12,
		"start_time": "pushed at",
		"end_time": "pushed at",
		"is_under_maintenance": false,
		"check_cert_expiry": false
	}`, string(pinned))
}
