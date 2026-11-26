package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// sendTestGrafanaOncallPayload sends a notification to a test server and returns the JSON body it got
func sendTestGrafanaOncallPayload(t *testing.T, message string, m *monitor.Model, hb *heartbeat.Model) map[string]any {
	t.Helper()
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewGrafanaOncallSender(zap.NewNop().Sugar(), NewHTTPClient())
	err := sender.Send(context.Background(), `{"grafana_oncall_url": "`+server.URL+`"}`, message, m, hb)
	require.NoError(t, err)
	return payload
}

func TestGrafanaOncallSender_Send_MonitorFields(t *testing.T) {
	beatTime := time.Date(2026, 2, 27, 0, 21, 0, 0, time.UTC)
	downBeat := &heartbeat.Model{
		Status:  shared.MonitorStatusDown,
		Msg:     `Get "https://192.168.1.10/health": dial tcp 192.168.1.10:443: connect: connection refused`,
		Ping:    685,
		Retries: 2,
		Time:    beatTime,
	}
	base := map[string]any{
		"title":        "API is down",
		"alert_uid":    "Peekaping/mon-1",
		"message":      downBeat.Msg,
		"state":        "alerting",
		"status":       "DOWN",
		"ping_ms":      float64(685),
		"retries":      float64(2),
		"time":         "2026-02-27T00:21:00Z",
		"monitor_id":   "mon-1",
		"monitor_type": "",
		"tags":         []any{"country:ua", "severity:critical"},
	}

	cases := []struct {
		name        string
		monitorType string
		config      string
		want        map[string]any // added to base
	}{
		{
			name:        "http url without credentials, query or fragment",
			monitorType: "http",
			config:      `{"url": "https://admin:s3cret@internal.example.com/health?api_key=abc#status"}`,
			want:        map[string]any{"monitor_url": "https://internal.example.com/health"},
		},
		{
			name:        "http-keyword url",
			monitorType: "http-keyword",
			config:      `{"url": "https://example.com/status?token=abc"}`,
			want:        map[string]any{"monitor_url": "https://example.com/status"},
		},
		{
			name:        "http-json-query url",
			monitorType: "http-json-query",
			config:      `{"url": "http://example.com:8080/api"}`,
			want:        map[string]any{"monitor_url": "http://example.com:8080/api"},
		},
		{
			name:        "http url that doesn't parse is left out",
			monitorType: "http",
			config:      `{"url": "https://exa mple.com/%zz"}`,
		},
		{
			name:        "tcp host",
			monitorType: "tcp",
			config:      `{"host": "db.example.com", "port": 5432}`,
			want:        map[string]any{"monitor_host": "db.example.com"},
		},
		{
			name:        "ping host",
			monitorType: "ping",
			config:      `{"host": "10.0.0.1", "packet_size": 32}`,
			want:        map[string]any{"monitor_host": "10.0.0.1"},
		},
		{
			name:        "dns host",
			monitorType: "dns",
			config:      `{"host": "example.com", "resolver_server": "1.1.1.1", "port": 53, "resolve_type": "A"}`,
			want:        map[string]any{"monitor_host": "example.com"},
		},
		{
			name:        "grpc target",
			monitorType: "grpc-keyword",
			config:      `{"grpcUrl": "grpc.example.com:50051", "grpcServiceName": "Health", "grpcMethod": "check"}`,
			want:        map[string]any{"monitor_host": "grpc.example.com:50051"},
		},
		{
			name:        "grpc URI target without credentials or query",
			monitorType: "grpc-keyword",
			config:      `{"grpcUrl": "dns://user:s3cret@1.1.1.1/grpc.example.com:443?token=abc"}`,
			want:        map[string]any{"monitor_host": "dns://1.1.1.1/grpc.example.com:443"},
		},
		{
			name:        "database DSN is left out",
			monitorType: "postgres",
			config:      `{"database_connection_string": "postgres://user:s3cret@db.example.com:5432/app", "host": "db.example.com"}`,
		},
		{
			name:        "push monitor has no target",
			monitorType: "push",
			config:      `{}`,
		},
		{
			name:        "config that isn't JSON",
			monitorType: "http",
			config:      `not json`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &monitor.Model{
				ID:     "mon-1",
				Name:   "API",
				Type:   tc.monitorType,
				Config: tc.config,
				Tags:   []string{"country:ua", "severity:critical"},
			}

			want := map[string]any{}
			for k, v := range base {
				want[k] = v
			}
			want["monitor_type"] = tc.monitorType
			for k, v := range tc.want {
				want[k] = v
			}

			assert.Equal(t, want, sendTestGrafanaOncallPayload(t, downBeat.Msg, m, downBeat))
		})
	}
}

func TestGrafanaOncallSender_Send_Up(t *testing.T) {
	m := &monitor.Model{ID: "mon-1", Name: "API", Type: "http", Config: `{"url": "https://example.com"}`, Tags: []string{"team:core"}}
	hb := &heartbeat.Model{Status: shared.MonitorStatusUp, Msg: "200 - 200 OK", Ping: 167, Time: time.Date(2026, 2, 27, 0, 25, 0, 0, time.UTC)}

	assert.Equal(t, map[string]any{
		"title":        "API is up",
		"alert_uid":    "Peekaping/mon-1",
		"message":      "200 - 200 OK",
		"state":        "ok",
		"status":       "UP",
		"ping_ms":      float64(167),
		"retries":      float64(0),
		"time":         "2026-02-27T00:25:00Z",
		"monitor_id":   "mon-1",
		"monitor_type": "http",
		"monitor_url":  "https://example.com",
		"tags":         []any{"team:core"},
	}, sendTestGrafanaOncallPayload(t, hb.Msg, m, hb))
}

// OnCall groups alerts by alert_uid and resolves a group when an alert in it has state ok, so each
// monitor needs a uid of its own, or one monitor's Up would resolve the alerts of every other
func TestGrafanaOncallSender_Send_AlertUIDIsPerMonitor(t *testing.T) {
	down := &heartbeat.Model{Status: shared.MonitorStatusDown, Msg: "connection refused", Time: time.Now()}
	up := &heartbeat.Model{Status: shared.MonitorStatusUp, Msg: "200 - OK", Time: time.Now()}
	pending := &heartbeat.Model{Status: shared.MonitorStatusPending, Msg: "retrying", Time: time.Now()}
	api := &monitor.Model{ID: "mon-1", Name: "API", Type: "http"}
	db := &monitor.Model{ID: "mon-2", Name: "DB", Type: "tcp"}

	apiDown := sendTestGrafanaOncallPayload(t, down.Msg, api, down)
	apiUp := sendTestGrafanaOncallPayload(t, up.Msg, api, up)
	apiPending := sendTestGrafanaOncallPayload(t, pending.Msg, api, pending)
	dbDown := sendTestGrafanaOncallPayload(t, down.Msg, db, down)

	assert.Equal(t, "Peekaping/mon-1", apiDown["alert_uid"])
	assert.Equal(t, apiDown["alert_uid"], apiUp["alert_uid"], "the Up has to resolve the alert group the Down opened")
	assert.Equal(t, apiDown["alert_uid"], apiPending["alert_uid"])
	assert.Equal(t, "Peekaping/mon-2", dbDown["alert_uid"])
	assert.NotEqual(t, apiDown["alert_uid"], dbDown["alert_uid"])
}

func TestGrafanaOncallSender_Send_UntaggedMonitorSendsEmptyTags(t *testing.T) {
	m := &monitor.Model{ID: "mon-1", Name: "API", Type: "push"}
	hb := &heartbeat.Model{Status: shared.MonitorStatusDown, Msg: "No heartbeat", Time: time.Now()}

	payload := sendTestGrafanaOncallPayload(t, hb.Msg, m, hb)

	assert.Equal(t, []any{}, payload["tags"])
}

func TestGrafanaOncallSender_Send_CertificateExpiry(t *testing.T) {
	m := &monitor.Model{
		ID:     "mon-1",
		Name:   "API",
		Type:   "http",
		Config: `{"url": "https://example.com/health"}`,
		Tags:   []string{"severity:warning"},
	}

	// The listener sends certificate expiry notifications without a heartbeat
	assert.Equal(t, map[string]any{
		"title":        "General notification",
		"message":      "Certificate Expiry Warning",
		"state":        "alerting",
		"monitor_id":   "mon-1",
		"monitor_type": "http",
		"monitor_url":  "https://example.com/health",
		"tags":         []any{"severity:warning"},
	}, sendTestGrafanaOncallPayload(t, "Certificate Expiry Warning", m, nil))
}

func TestGrafanaOncallSender_Send_TestNotificationLeavesOutZeroFields(t *testing.T) {
	// The same monitor and heartbeat as the notification channel controller's Test endpoint
	m := &monitor.Model{Name: "Test Monitor", Type: "http"}
	hb := &heartbeat.Model{Status: shared.MonitorStatusDown, Msg: "This is a test notification from Peekaping"}

	assert.Equal(t, map[string]any{
		"title":        "Test Monitor is down",
		"message":      "This is a test notification from Peekaping",
		"state":        "alerting",
		"status":       "DOWN",
		"ping_ms":      float64(0),
		"retries":      float64(0),
		"monitor_type": "http",
		"tags":         []any{},
	}, sendTestGrafanaOncallPayload(t, hb.Msg, m, hb))
}

func TestGrafanaOncallSender_Send_GeneralNotificationWithoutMonitor(t *testing.T) {
	assert.Equal(t, map[string]any{
		"title":   "General notification",
		"message": "hello",
		"state":   "alerting",
	}, sendTestGrafanaOncallPayload(t, "hello", nil, nil))
}

func TestURLWithoutSecrets(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"https://example.com/health", "https://example.com/health"},
		{"https://user:pass@example.com/health", "https://example.com/health"},
		{"https://user@example.com", "https://example.com"},
		{"https://example.com/health?api_key=abc&x=1", "https://example.com/health"},
		{"https://example.com/health?", "https://example.com/health"},
		{"https://example.com/#token=abc", "https://example.com/"},
		{"https://[::1]:8443/health", "https://[::1]:8443/health"},
		{"", ""},
		{"example.com/health", ""},
		{"https:user:pass@example.com", ""},
		{"https://exa mple.com", ""},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			assert.Equal(t, tc.want, urlWithoutSecrets(tc.raw))
		})
	}
}

func TestGRPCTargetWithoutSecrets(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"grpc.example.com:50051", "grpc.example.com:50051"},
		{"10.0.0.1:50051", "10.0.0.1:50051"},
		{"[::1]:50051", "[::1]:50051"},
		{"dns:///grpc.example.com:443", "dns:///grpc.example.com:443"},
		{"unix:///run/grpc.sock", "unix:///run/grpc.sock"},
		{"dns://user:pass@1.1.1.1/grpc.example.com:443", "dns://1.1.1.1/grpc.example.com:443"},
		{"dns:///grpc.example.com:443?token=abc#x", "dns:///grpc.example.com:443"},
		{"", ""},
		{"dns://exa mple.com/x", ""},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			assert.Equal(t, tc.want, grpcTargetWithoutSecrets(tc.raw))
		})
	}
}
