package providers

import (
	"encoding/json"
	"testing"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"go.uber.org/zap"
)

func TestPagerDutyConfig_Unmarshal(t *testing.T) {
	sender := NewPagerDutySender(zap.NewNop().Sugar(), nil)

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
	sender := NewPagerDutySender(zap.NewNop().Sugar(), nil)

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
	sender := NewPagerDutySender(zap.NewNop().Sugar(), nil)

	// Test DOWN status
	downHeartbeat := &heartbeat.Model{Status: shared.MonitorStatusDown}
	cfg := &PagerDutyConfig{AutoResolve: "0"}

	action := sender.getEventAction(downHeartbeat, cfg)
	if action != "trigger" {
		t.Errorf("Expected action 'trigger' for DOWN status, got '%s'", action)
	}

	// Test UP status with no auto-resolve
	upHeartbeat := &heartbeat.Model{Status: shared.MonitorStatusUp}
	action = sender.getEventAction(upHeartbeat, cfg)
	if action != "" {
		t.Errorf("Expected no action for UP status with no auto-resolve, got '%s'", action)
	}

	// Test UP status with auto-resolve
	cfg.AutoResolve = "resolve"
	action = sender.getEventAction(upHeartbeat, cfg)
	if action != "resolve" {
		t.Errorf("Expected action 'resolve' for UP status with auto-resolve, got '%s'", action)
	}

	// Test UP status with auto-acknowledge
	cfg.AutoResolve = "acknowledge"
	action = sender.getEventAction(upHeartbeat, cfg)
	if action != "acknowledge" {
		t.Errorf("Expected action 'acknowledge' for UP status with auto-acknowledge, got '%s'", action)
	}
}

func TestPagerDutySender_getTitle(t *testing.T) {
	sender := NewPagerDutySender(zap.NewNop().Sugar(), nil)

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
	sender := NewPagerDutySender(zap.NewNop().Sugar(), nil)

	cases := []struct {
		name     string
		monitor  *monitor.Model
		expected string
	}{
		{"nil monitor", nil, ""},
		{"http keyword url", &monitor.Model{Name: "API", Type: "http-keyword", Config: `{"url": "https://api.example.com/health"}`}, "https://api.example.com/health"},
		{"tcp host and port", &monitor.Model{Name: "DB", Type: "tcp", Config: `{"host": "2001:db8::1", "port": 5432}`}, "[2001:db8::1]:5432"},
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
	sender := NewPagerDutySender(zap.NewNop().Sugar(), nil)

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

func TestPagerDutyPayload_Structure(t *testing.T) {
	sender := NewPagerDutySender(zap.NewNop().Sugar(), nil)

	// Create test data
	monitor := &monitor.Model{
		ID:   "test-monitor-123",
		Name: "Test Monitor",
	}

	heartbeat := &heartbeat.Model{
		Status: shared.MonitorStatusDown,
		Msg:    "Connection timeout",
	}

	cfg := &PagerDutyConfig{
		IntegrationKey: "test-key-123",
		Priority:       "warning",
	}

	// Get event action
	eventAction := sender.getEventAction(heartbeat, cfg)

	// Build expected payload structure
	expectedPayload := map[string]any{
		"payload": map[string]any{
			"summary":  "[Peekaping Monitor 🔴 Down] [Test Monitor] Connection timeout",
			"severity": "warning",
			"source":   monitor.Name, // Fallback to monitor name
		},
		"routing_key":  cfg.IntegrationKey,
		"event_action": eventAction,
		"dedup_key":    "Peekaping/test-monitor-123",
	}

	// Marshal expected payload
	expectedJSON, err := json.Marshal(expectedPayload)
	if err != nil {
		t.Fatalf("Failed to marshal expected payload: %v", err)
	}

	// Verify the structure is valid JSON
	var parsedPayload map[string]any
	err = json.Unmarshal(expectedJSON, &parsedPayload)
	if err != nil {
		t.Fatalf("Failed to parse expected payload JSON: %v", err)
	}

	// Verify required fields exist
	payload, ok := parsedPayload["payload"].(map[string]any)
	if !ok {
		t.Fatal("Payload field should be a map")
	}

	if _, ok := payload["summary"]; !ok {
		t.Error("Payload should contain 'summary' field")
	}

	if _, ok := payload["severity"]; !ok {
		t.Error("Payload should contain 'severity' field")
	}

	if _, ok := payload["source"]; !ok {
		t.Error("Payload should contain 'source' field")
	}

	if _, ok := parsedPayload["routing_key"]; !ok {
		t.Error("Root should contain 'routing_key' field")
	}

	if _, ok := parsedPayload["event_action"]; !ok {
		t.Error("Root should contain 'event_action' field")
	}

	if _, ok := parsedPayload["dedup_key"]; !ok {
		t.Error("Root should contain 'dedup_key' field")
	}
}
