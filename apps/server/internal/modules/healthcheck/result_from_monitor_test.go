package healthcheck

import (
	"testing"
	"time"

	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestResultFromMonitor_CarriesTheMonitorSettingsAndTheCheck(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	end := start.Add(12 * time.Millisecond)
	tlsInfo := &certificate.TLSInfo{Valid: true}
	m := &monitor.Model{
		ID:             "mon-1",
		Name:           "Checkout",
		Type:           "http",
		Interval:       60,
		Timeout:        48,
		MaxRetries:     3,
		RetryInterval:  30,
		ResendInterval: 5,
		Config:         `{"url":"https://example.com","check_cert_expiry":true}`,
	}

	got := ResultFromMonitor(m, false, shared.MonitorStatusDown, "503 - Service Unavailable", 12, start, end, tlsInfo, zap.NewNop().Sugar())

	assert.Equal(t, &ingester.Result{
		ProbeID:            "local",
		MonitorID:          "mon-1",
		MonitorName:        "Checkout",
		MonitorType:        "http",
		MonitorInterval:    60,
		MonitorTimeout:     48,
		MonitorMaxRetries:  3,
		MonitorRetryInt:    30,
		MonitorResendInt:   5,
		MonitorConfig:      `{"url":"https://example.com","check_cert_expiry":true}`,
		Status:             shared.MonitorStatusDown,
		Message:            "503 - Service Unavailable",
		PingMs:             12,
		StartTime:          start,
		EndTime:            end,
		IsUnderMaintenance: false,
		TLSInfo:            tlsInfo,
		CheckCertExpiry:    true,
	}, got)
}

func TestResultFromMonitor_AMonitorUnderMaintenanceReportsMaintenance(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	m := &monitor.Model{ID: "mon-1", Type: "http"}

	got := ResultFromMonitor(m, true, shared.MonitorStatusDown, "connection refused", 250, start, start, nil, zap.NewNop().Sugar())

	assert.True(t, got.IsUnderMaintenance)
	assert.Equal(t, shared.MonitorStatusMaintenance, got.Status, "planned downtime is neither Up nor Down")
	assert.Equal(t, "Monitor under maintenance", got.Message, "the message a scheduled check records in a maintenance window")
	assert.Zero(t, got.PingMs, "and a Maintenance heartbeat has no response time, as a scheduled check's")
}

func TestResultFromMonitor_ReadsTheCertificateExpiryFlagLikeTheCheckLoader(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config string
		want   bool
	}{
		{name: "on in the config", config: `{"url": "https://example.com", "check_cert_expiry": true}`, want: true},
		{name: "off in the config", config: `{"check_cert_expiry": false}`, want: false},
		{name: "absent from the config", config: `{"url": "https://example.com"}`, want: false},
		{name: "no config", config: "", want: false},
		{name: "a config that isn't JSON", config: `{`, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := &monitor.Model{ID: "mon-1", Type: "http", Config: tt.config}

			got := ResultFromMonitor(m, false, shared.MonitorStatusUp, "OK", 1, time.Now(), time.Now(), nil, zap.NewNop().Sugar())

			assert.Equal(t, tt.want, got.CheckCertExpiry)
		})
	}
}
