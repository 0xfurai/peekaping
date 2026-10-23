package ingester

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_IngestStoresTheResultAndPublishesItsEvents(t *testing.T) {
	ti := newTestIngester()
	result := checkResult(monitorSettings{}, down)

	require.NoError(t, ti.service.Ingest(context.Background(), &result))

	got := ti.heartbeats.latest()
	require.NotNil(t, got)
	assert.Equal(t, monitorID, got.MonitorID)
	assert.Equal(t, down, got.Status)
	assert.Equal(t, "check message", got.Msg)
	assert.Equal(t, 12, got.Ping)
	assert.Equal(t, result.StartTime, got.Time)
	assert.Equal(t, result.EndTime, got.EndTime)
	assert.Equal(t, []string{events.MonitorStatusChanged.Name(), events.ImportantHeartbeat.Name()}, ti.bus.TakeTopics())
}

func TestService_IngestSeesThePreviousResult(t *testing.T) {
	ti := newTestIngester()
	monitor := monitorSettings{maxRetries: 1}

	first, second := checkResult(monitor, up), checkResult(monitor, down)
	require.NoError(t, ti.service.Ingest(context.Background(), &first))
	require.NoError(t, ti.service.Ingest(context.Background(), &second))

	require.Len(t, ti.heartbeats.stored, 2)
	assert.Equal(t, pending, ti.heartbeats.latest().Status, "the first failure is Pending while retries are left")
}

func TestService_IngestReturnsAFailedWrite(t *testing.T) {
	ti := newTestIngester()
	ti.heartbeats.createErr = assert.AnError
	result := checkResult(monitorSettings{}, up)

	assert.ErrorIs(t, ti.service.Ingest(context.Background(), &result), assert.AnError)
	assert.Empty(t, ti.bus.TakeTopics())
}

func TestResult_DecodesTheIngesterTaskJSON(t *testing.T) {
	// The payload as the worker, the push endpoint and the probe protocol enqueue it
	body := `{
		"probe_id": "probe-1",
		"monitor_id": "mon-1",
		"monitor_name": "Test",
		"monitor_type": "http",
		"monitor_interval": 60,
		"monitor_timeout": 48,
		"monitor_max_retries": 3,
		"monitor_retry_interval": 30,
		"monitor_resend_interval": 5,
		"monitor_config": "{\"url\":\"https://example.com\"}",
		"status": 1,
		"message": "200 - OK",
		"ping_ms": 12,
		"start_time": "2026-10-17T12:00:00Z",
		"end_time": "2026-10-17T12:00:01Z",
		"is_under_maintenance": true,
		"tls_info": {"valid": true},
		"check_cert_expiry": true
	}`

	var got Result
	require.NoError(t, json.Unmarshal([]byte(body), &got))

	assert.Equal(t, Result{
		ProbeID:            "probe-1",
		MonitorID:          "mon-1",
		MonitorName:        "Test",
		MonitorType:        "http",
		MonitorInterval:    60,
		MonitorTimeout:     48,
		MonitorMaxRetries:  3,
		MonitorRetryInt:    30,
		MonitorResendInt:   5,
		MonitorConfig:      `{"url":"https://example.com"}`,
		Status:             up,
		Message:            "200 - OK",
		PingMs:             12,
		StartTime:          time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
		EndTime:            time.Date(2026, 10, 17, 12, 0, 1, 0, time.UTC),
		IsUnderMaintenance: true,
		TLSInfo:            &certificate.TLSInfo{Valid: true},
		CheckCertExpiry:    true,
	}, got)
}
