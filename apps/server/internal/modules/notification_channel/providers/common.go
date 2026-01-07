package providers

import (
	"encoding/json"
	"fmt"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/utils"
)

func GenericValidator[T any](cfg *T) error {
	return utils.Validate.Struct(cfg)
}

func GenericUnmarshal[T any](configJSON string) (*T, error) {
	var cfg T
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return &cfg, nil
}

// PrepareTemplateBindings converts monitor and heartbeat objects to JSON representation
// and prepares template bindings with parsed config for template engines
func PrepareTemplateBindings(monitor *monitor.Model, heartbeat *heartbeat.Model, message string) map[string]any {
	bindings := map[string]any{}

	if monitor != nil {
		// Convert monitor to JSON representation for template
		monitorJSON := map[string]any{}
		monitorBytes, _ := json.Marshal(monitor)
		json.Unmarshal(monitorBytes, &monitorJSON)

		// Parse config JSON string to make nested properties accessible
		if configStr, ok := monitorJSON["config"].(string); ok && configStr != "" {
			var configJSON map[string]any
			if err := json.Unmarshal([]byte(configStr), &configJSON); err == nil {
				monitorJSON["config"] = configJSON
			}
			// If parsing fails, keep the original string value
		}

		bindings["monitor"] = monitorJSON
		// Use JSON field name for consistency
		if name, ok := monitorJSON["name"].(string); ok {
			bindings["name"] = name
		}
	}

	if heartbeat != nil {
		// Convert heartbeat to JSON representation for template
		heartbeatJSON := map[string]any{}
		heartbeatBytes, _ := json.Marshal(heartbeat)
		json.Unmarshal(heartbeatBytes, &heartbeatJSON)
		bindings["heartbeat"] = heartbeatJSON
		bindings["status"] = humanReadableStatus(int(heartbeat.Status))
		bindings["status_icon"] = humanReadableStatusIcons(int(heartbeat.Status))
		bindings["status_color"] = humanReadableStatusColor(int(heartbeat.Status))
		bindings["ping"] = heartbeat.Ping
		bindings["time"] = heartbeat.Time

	}

	bindings["msg"] = message

	return bindings
}

func humanReadableStatus(status int) string {
	switch status {
	case 0:
		return "DOWN"
	case 1:
		return "UP"
	case 2:
		return "PENDING"
	case 3:
		return "MAINTENANCE"
	default:
		return fmt.Sprintf("Unknown (%d)", status)
	}
}

func humanReadableStatusIcons(status int) string {
	switch status {
	case 0:
		return "❌"
	case 1:
		return "✅"
	case 2:
		return "⏳"
	case 3:
		return "🚧"
	default:
		return "❔"
	}
}

func humanReadableStatusColor(status int) int {
	switch status {
	case 0:
		return 15680580
	case 1:
		return 2278750
	case 2:
		return 11032055
	case 3:
		return 3900150
	default:
		return 7041664
	}
}
