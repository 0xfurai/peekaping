package providers

import (
	"testing"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"go.uber.org/zap"
)

func TestPagerDutyConfig_Unmarshal(t *testing.T) {
	sender := NewPagerDutySender(zap.NewNop().Sugar(), nil, NewHTTPClient())

	// Test valid config
	validConfig := `{
		"pagerduty_integration_key": "test-key-123",
		"pagerduty_integration_url": "https://events.pagerduty.com/v2/enqueue",
		"pagerduty_priority": "warning",
		"pagerduty_auto_resolve": "resolve"
	}`

	cfg, err := sender.Unmarshal(validConfig)
	if err != nil {
		t.Fatalf("Failed to unmarshal valid config: %v", err)
	}

	pagerDutyConfig, ok := cfg.(*PagerDutyConfig)
	if !ok {
		t.Fatal("Failed to cast to PagerDutyConfig")
	}

	if pagerDutyConfig.IntegrationKey != "test-key-123" {
		t.Errorf("Expected integration key 'test-key-123', got '%s'", pagerDutyConfig.IntegrationKey)
	}

	if pagerDutyConfig.IntegrationURL != "https://events.pagerduty.com/v2/enqueue" {
		t.Errorf("Expected integration URL 'https://events.pagerduty.com/v2/enqueue', got '%s'", pagerDutyConfig.IntegrationURL)
	}

	if pagerDutyConfig.Priority != "warning" {
		t.Errorf("Expected priority 'warning', got '%s'", pagerDutyConfig.Priority)
	}

	if pagerDutyConfig.AutoResolve != "resolve" {
		t.Errorf("Expected auto resolve 'resolve', got '%s'", pagerDutyConfig.AutoResolve)
	}
}

func TestPagerDutyConfig_Validate(t *testing.T) {
	sender := NewPagerDutySender(zap.NewNop().Sugar(), nil, NewHTTPClient())

	// Test valid config
	validConfig := `{
		"pagerduty_integration_key": "test-key-123",
		"pagerduty_integration_url": "https://events.pagerduty.com/v2/enqueue"
	}`

	err := sender.Validate(validConfig)
	if err != nil {
		t.Fatalf("Valid config should not return error: %v", err)
	}

	// Test invalid config (missing required fields)
	invalidConfig := `{
		"pagerduty_integration_url": "https://events.pagerduty.com/v2/enqueue"
	}`

	err = sender.Validate(invalidConfig)
	if err == nil {
		t.Fatal("Invalid config should return error")
	}

	// Test invalid URL
	invalidURLConfig := `{
		"pagerduty_integration_key": "test-key-123",
		"pagerduty_integration_url": "not-a-valid-url"
	}`

	err = sender.Validate(invalidURLConfig)
	if err == nil {
		t.Fatal("Invalid URL should return error")
	}
}

func TestPagerDutySender_getEventAction(t *testing.T) {
	sender := NewPagerDutySender(zap.NewNop().Sugar(), nil, NewHTTPClient())

	// Test DOWN status
	downHeartbeat := &heartbeat.Model{Status: shared.MonitorStatusDown}
	cfg := &PagerDutyConfig{AutoResolve: "0"}

	action := sender.getEventAction(nil, downHeartbeat, cfg)
	if action != "trigger" {
		t.Errorf("Expected action 'trigger' for DOWN status, got '%s'", action)
	}

	// Test UP status with no auto-resolve
	upHeartbeat := &heartbeat.Model{Status: shared.MonitorStatusUp}
	action = sender.getEventAction(nil, upHeartbeat, cfg)
	if action != "" {
		t.Errorf("Expected no action for UP status with no auto-resolve, got '%s'", action)
	}

	// Test UP status with auto-resolve
	cfg.AutoResolve = "resolve"
	action = sender.getEventAction(nil, upHeartbeat, cfg)
	if action != "resolve" {
		t.Errorf("Expected action 'resolve' for UP status with auto-resolve, got '%s'", action)
	}

	// Test UP status with auto-acknowledge
	cfg.AutoResolve = "acknowledge"
	action = sender.getEventAction(nil, upHeartbeat, cfg)
	if action != "acknowledge" {
		t.Errorf("Expected action 'acknowledge' for UP status with auto-acknowledge, got '%s'", action)
	}
}

func TestPagerDutySender_getEventActionWithoutAHeartbeat(t *testing.T) {
	sender := NewPagerDutySender(zap.NewNop().Sugar(), nil, NewHTTPClient())
	probeOffline := &monitor.Model{ID: "probe-1", Type: shared.MonitorTypeProbe, Name: "Probe Frankfurt", Status: shared.MonitorStatusDown}
	probeOnline := &monitor.Model{ID: "probe-1", Type: shared.MonitorTypeProbe, Name: "Probe Frankfurt", Status: shared.MonitorStatusUp}
	// A certificate expiry or a stale-monitor notification has no heartbeat either, and what its
	// monitor's status says is not what it is about
	upMonitor := &monitor.Model{ID: "mon-1", Type: "http", Name: "Public API", Status: shared.MonitorStatusUp}

	cases := []struct {
		name        string
		monitor     *monitor.Model
		autoResolve string
		want        string
	}{
		{"a test notification triggers", nil, "resolve", "trigger"},
		{"a notification about a monitor triggers", upMonitor, "resolve", "trigger"},

		{"a probe offline triggers with no auto-resolve", probeOffline, "", "trigger"},
		{"a probe offline triggers with auto-resolve 0", probeOffline, "0", "trigger"},
		{"a probe offline triggers with auto-acknowledge", probeOffline, "acknowledge", "trigger"},
		{"a probe offline triggers with auto-resolve", probeOffline, "resolve", "trigger"},

		{"a probe online sends nothing with no auto-resolve", probeOnline, "", ""},
		{"a probe online sends nothing with auto-resolve 0", probeOnline, "0", ""},
		{"a probe online acknowledges with auto-acknowledge", probeOnline, "acknowledge", "acknowledge"},
		{"a probe online resolves with auto-resolve", probeOnline, "resolve", "resolve"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sender.getEventAction(tc.monitor, nil, &PagerDutyConfig{AutoResolve: tc.autoResolve})
			if got != tc.want {
				t.Errorf("Expected action %q, got %q", tc.want, got)
			}
		})
	}
}

func TestPagerDutySender_getTitle(t *testing.T) {
	sender := NewPagerDutySender(zap.NewNop().Sugar(), nil, NewHTTPClient())

	// Test DOWN status
	downHeartbeat := &heartbeat.Model{Status: shared.MonitorStatusDown}
	title := sender.getTitle(downHeartbeat)
	expected := "Peekaping Monitor 🔴 Down"
	if title != expected {
		t.Errorf("Expected title '%s', got '%s'", expected, title)
	}

	// Test UP status
	upHeartbeat := &heartbeat.Model{Status: shared.MonitorStatusUp}
	title = sender.getTitle(upHeartbeat)
	expected = "Peekaping Monitor ✅ Up"
	if title != expected {
		t.Errorf("Expected title '%s', got '%s'", expected, title)
	}

	// Test PENDING status
	pendingHeartbeat := &heartbeat.Model{Status: shared.MonitorStatusPending}
	title = sender.getTitle(pendingHeartbeat)
	expected = "Peekaping Monitor ⏳ Pending"
	if title != expected {
		t.Errorf("Expected title '%s', got '%s'", expected, title)
	}

	// Test MAINTENANCE status
	maintenanceHeartbeat := &heartbeat.Model{Status: shared.MonitorStatusMaintenance}
	title = sender.getTitle(maintenanceHeartbeat)
	expected = "Peekaping Monitor 🔧 Maintenance"
	if title != expected {
		t.Errorf("Expected title '%s', got '%s'", expected, title)
	}

	// Test nil heartbeat
	title = sender.getTitle(nil)
	expected = "Peekaping Alert"
	if title != expected {
		t.Errorf("Expected title '%s', got '%s'", expected, title)
	}
}

func TestPagerDutySender_getMonitorURL(t *testing.T) {
	sender := NewPagerDutySender(zap.NewNop().Sugar(), nil, NewHTTPClient())

	cases := []struct {
		name     string
		monitor  *monitor.Model
		expected string
	}{
		{"nil monitor", nil, ""},
		{"http keyword url", &monitor.Model{Name: "API", Type: "http-keyword", Config: `{"url": "https://api.example.com/health"}`}, "https://api.example.com/health"},
		{"tcp host and port", &monitor.Model{Name: "DB", Type: "tcp", Config: `{"host": "2001:db8::1", "port": 5432}`}, "[2001:db8::1]:5432"},
		{"ping host", &monitor.Model{Name: "Ping", Type: "ping", Config: `{"host": "192.0.2.1"}`}, "192.0.2.1"},
		{"falls back to the name", &monitor.Model{Name: "Push", Type: "push", Config: `{"pushToken": "x"}`}, "Push"},
		{"invalid config", &monitor.Model{Name: "Broken", Type: "http", Config: `not json`}, "Broken"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sender.getMonitorURL(tc.monitor); got != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, got)
			}
		})
	}
}

func TestPagerDutySender_getSeverity(t *testing.T) {
	sender := NewPagerDutySender(zap.NewNop().Sugar(), nil, NewHTTPClient())

	// Test default severity
	cfg := &PagerDutyConfig{}
	severity := sender.getSeverity(cfg)
	if severity != "warning" {
		t.Errorf("Expected default severity 'warning', got '%s'", severity)
	}

	// Test custom severity
	cfg.Priority = "critical"
	severity = sender.getSeverity(cfg)
	if severity != "critical" {
		t.Errorf("Expected severity 'critical', got '%s'", severity)
	}
}
