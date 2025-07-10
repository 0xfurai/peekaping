package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"peekaping/src/config"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"peekaping/src/modules/shared"
	"peekaping/src/version"
	"strings"

	"go.uber.org/zap"
)

// PagerDutyConfig holds the configuration for PagerDuty notifications
type PagerDutyConfig struct {
	IntegrationKey string `json:"pagerduty_integration_key" validate:"required"`
	IntegrationURL string `json:"pagerduty_integration_url" validate:"required,url"`
	Priority       string `json:"pagerduty_priority"`
	AutoResolve    string `json:"pagerduty_auto_resolve"`
}

// PagerDutySender handles sending notifications to PagerDuty
type PagerDutySender struct {
	logger *zap.SugaredLogger
	config *config.Config
}

// NewPagerDutySender creates a new PagerDutySender
func NewPagerDutySender(logger *zap.SugaredLogger, config *config.Config) *PagerDutySender {
	return &PagerDutySender{logger: logger, config: config}
}

func (p *PagerDutySender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[PagerDutyConfig](configJSON)
}

func (p *PagerDutySender) Validate(configJSON string) error {
	cfg, err := p.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*PagerDutyConfig))
}

// getMonitorURL extracts the URL from monitor for PagerDuty source field
func (p *PagerDutySender) getMonitorURL(monitor *monitor.Model) string {
	if monitor == nil {
		return ""
	}

	// Try to extract URL from monitor config JSON
	if monitor.Config != "" {
		var config map[string]any
		if err := json.Unmarshal([]byte(monitor.Config), &config); err == nil {
			// Handle different monitor types
			switch monitor.Type {
			case "http":
				// HTTP monitors have a 'url' field
				if url, ok := config["url"].(string); ok && url != "" {
					return url
				}
			case "tcp":
				// TCP monitors have 'host' and 'port' fields
				if hostname, ok := config["host"].(string); ok && hostname != "" {
					if port, ok := config["port"].(float64); ok && port > 0 {
						return fmt.Sprintf("%s:%.0f", hostname, port)
					}
					return hostname
				}
			case "ping":
				// Ping monitors have a 'host' field
				if hostname, ok := config["host"].(string); ok && hostname != "" {
					return hostname
				}
			case "dns":
				// DNS monitors have a 'host' field
				if hostname, ok := config["host"].(string); ok && hostname != "" {
					return hostname
				}
			}
		}
	}

	// Fallback to monitor name if no URL/hostname found
	return monitor.Name
}

// getEventAction determines the PagerDuty event action based on heartbeat status and config
func (p *PagerDutySender) getEventAction(heartbeat *heartbeat.Model, cfg *PagerDutyConfig) string {
	if heartbeat == nil {
		return "trigger"
	}

	switch heartbeat.Status {
	case shared.MonitorStatusUp:
		if cfg.AutoResolve == "acknowledge" {
			return "acknowledge"
		} else if cfg.AutoResolve == "resolve" {
			return "resolve"
		}
		return "" // No action for UP status unless auto-resolve is configured
	case shared.MonitorStatusDown:
		return "trigger"
	default:
		return "trigger"
	}
}

// getTitle generates the title for the PagerDuty alert
func (p *PagerDutySender) getTitle(heartbeat *heartbeat.Model) string {
	if heartbeat == nil {
		return "Peekaping Alert"
	}

	switch heartbeat.Status {
	case shared.MonitorStatusUp:
		return "Peekaping Monitor ✅ Up"
	case shared.MonitorStatusDown:
		return "Peekaping Monitor 🔴 Down"
	case shared.MonitorStatusPending:
		return "Peekaping Monitor ⏳ Pending"
	case shared.MonitorStatusMaintenance:
		return "Peekaping Monitor 🔧 Maintenance"
	default:
		return "Peekaping Alert"
	}
}

// getSeverity determines the severity level for PagerDuty
func (p *PagerDutySender) getSeverity(cfg *PagerDutyConfig) string {
	if cfg.Priority == "" {
		return "warning"
	}
	return cfg.Priority
}

func (p *PagerDutySender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	monitor *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := p.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	cfg := cfgAny.(*PagerDutyConfig)

	// Debug logging
	jsonDebug, _ := json.MarshalIndent(cfg, "", "  ")
	p.logger.Debugf("PagerDuty config: %s", string(jsonDebug))
	p.logger.Infof("Sending PagerDuty notification to: %s", cfg.IntegrationURL)

	// Determine event action
	eventAction := p.getEventAction(heartbeat, cfg)
	if eventAction == "" {
		p.logger.Infof("No action required for PagerDuty notification")
		return nil
	}

	// Generate title
	title := p.getTitle(heartbeat)

	// Get monitor URL for source field
	monitorURL := p.getMonitorURL(monitor)
	if monitorURL == "" && monitor != nil {
		// Fallback to monitor name if URL is not available
		monitorURL = monitor.Name
	}

	// Prepare PagerDuty payload
	payload := map[string]any{
		"payload": map[string]any{
			"summary":  fmt.Sprintf("[%s] [%s] %s", title, monitor.Name, message),
			"severity": p.getSeverity(cfg),
			"source":   monitorURL,
		},
		"routing_key":  cfg.IntegrationKey,
		"event_action": eventAction,
		"dedup_key":    fmt.Sprintf("Peekaping/%s", monitor.ID),
	}

	// Add client information if base URL is available
	if p.config.ClientURL != "" && monitor != nil {
		payload["client"] = "Peekaping"
		payload["client_url"] = fmt.Sprintf("%s/monitors/%s", strings.TrimRight(p.config.ClientURL, "/"), monitor.ID)
	}

	// Convert payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal PagerDuty payload: %w", err)
	}

	p.logger.Debugf("PagerDuty payload: %s", string(jsonPayload))

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.IntegrationURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping-PagerDuty/"+version.Version)

	p.logger.Debugf("Sending PagerDuty request: %s", req.URL.String())

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send PagerDuty notification: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("PagerDuty notification failed with status code: %d", resp.StatusCode)
	}

	p.logger.Infof("PagerDuty notification sent successfully")
	return nil
}
