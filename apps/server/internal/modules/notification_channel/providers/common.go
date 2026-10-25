package providers

import (
	"encoding/json"
	"fmt"
	"net"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/validation"
	"strconv"
	"strings"
)

func GenericValidator[T any](cfg *T) error {
	return validation.Validate.Struct(cfg)
}

func GenericUnmarshal[T any](configJSON string) (*T, error) {
	var cfg T
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return &cfg, nil
}

// unknownMonitorName names the monitor of a notification that comes with none
const unknownMonitorName = "Unknown Monitor"

// peekapingLink returns the page of Peekaping that a notification links to: the Probes page for a
// probe's notification, whose stand-in monitor has no page of its own, and otherwise the
// monitor's page. baseURL is where the web app is served; with none, or with no monitor, there is
// no link and it returns "".
func peekapingLink(baseURL string, m *monitor.Model) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" || m == nil {
		return ""
	}
	if m.IsProbeEvent() {
		return baseURL + "/probes"
	}
	return baseURL + "/monitors/" + m.ID
}

// monitorAddress returns what a monitor checks: the URL of an HTTP monitor, or the host of a
// TCP, ping or DNS monitor (with the port for TCP). It returns "" for other monitor types.
func monitorAddress(m *monitor.Model) string {
	if m == nil || m.Config == "" {
		return ""
	}

	var cfg struct {
		URL  string `json:"url"`
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := json.Unmarshal([]byte(m.Config), &cfg); err != nil {
		return ""
	}

	switch m.Type {
	case "http", "http-keyword", "http-json-query":
		return cfg.URL
	case "tcp":
		if cfg.Host != "" && cfg.Port > 0 {
			return net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
		}
		return cfg.Host
	case "ping", "dns":
		// A DNS monitor's port belongs to its resolver, not the host
		return cfg.Host
	default:
		return ""
	}
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
		// Where the monitor is Down, for templates: the names of the locations that are, and how many
		// the monitor has
		bindings["locations_down"] = LocationsDown(monitor)
		bindings["locations_total"] = LocationsTotal(monitor)
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
