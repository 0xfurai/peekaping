package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"peekaping/internal/config"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/version"

	"go.uber.org/zap"
)

// PagerDutyConfig holds the configuration for PagerDuty notifications
type PagerDutyConfig struct {
	IntegrationKey string `json:"pagerduty_integration_key" validate:"required" secret:"true"`
	IntegrationURL string `json:"pagerduty_integration_url" validate:"required,url" endpoint:"true"`
	Priority       string `json:"pagerduty_priority"`
	AutoResolve    string `json:"pagerduty_auto_resolve"`
}

// PagerDutySender handles sending notifications to PagerDuty
type PagerDutySender struct {
	logger *zap.SugaredLogger
	client *http.Client
	config *config.Config
}

// NewPagerDutySender creates a new PagerDutySender
func NewPagerDutySender(logger *zap.SugaredLogger, config *config.Config, client *http.Client) *PagerDutySender {
	return &PagerDutySender{logger: logger, config: config, client: client}
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

// getMonitorURL returns what the monitor checks for the PagerDuty source field, or its name
func (p *PagerDutySender) getMonitorURL(monitor *monitor.Model) string {
	if monitor == nil {
		return ""
	}
	if address := monitorAddress(monitor); address != "" {
		return address
	}
	return monitor.Name
}

// getEventAction determines the PagerDuty event action based on heartbeat status and config. A
// probe's notification has no heartbeat, so the status of its stand-in monitor says whether the
// probe went offline or came back online. Any other notification without a heartbeat triggers.
func (p *PagerDutySender) getEventAction(monitor *monitor.Model, heartbeat *heartbeat.Model, cfg *PagerDutyConfig) string {
	switch {
	case heartbeat != nil:
		return eventActionForStatus(heartbeat.Status, cfg)
	case monitor.IsProbeEvent():
		return eventActionForStatus(monitor.Status, cfg)
	default:
		return "trigger"
	}
}

// eventActionForStatus is the event action for a monitor, or a probe, that has this status
func eventActionForStatus(status shared.MonitorStatus, cfg *PagerDutyConfig) string {
	switch status {
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

	p.logger.Infof("Sending PagerDuty notification to: %s", cfg.IntegrationURL)

	// Determine event action
	eventAction := p.getEventAction(monitor, heartbeat, cfg)
	if eventAction == "" {
		p.logger.Infof("No action required for PagerDuty notification")
		return nil
	}

	// Generate title
	title := p.getTitle(heartbeat)

	// Get monitor URL for source field
	monitorURL := p.getMonitorURL(monitor)
	monitorName := unknownMonitorName
	if monitor != nil {
		monitorName = monitor.Name
	}
	if monitorURL == "" {
		// Fallback to monitor name if URL is not available. A notification with no monitor has
		// Peekaping as its source, since PagerDuty needs one.
		monitorURL = "Peekaping"
		if monitor != nil {
			monitorURL = monitor.Name
		}
	}

	// Prepare PagerDuty payload
	payload := map[string]any{
		"payload": map[string]any{
			"summary":  fmt.Sprintf("[%s] [%s] %s", title, monitorName, message),
			"severity": p.getSeverity(cfg),
			"source":   monitorURL,
		},
		"routing_key":  cfg.IntegrationKey,
		"event_action": eventAction,
	}
	// Alerts of one monitor, or one probe, are one incident. With no monitor, PagerDuty makes up a
	// key for a trigger, so each is an incident of its own.
	if monitor != nil {
		payload["dedup_key"] = fmt.Sprintf("Peekaping/%s", monitor.ID)
	}

	// Add client information if base URL is available
	if link := peekapingLink(p.config.ClientURL, monitor); link != "" {
		payload["client"] = "Peekaping"
		payload["client_url"] = link
	}

	// Convert payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal PagerDuty payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.IntegrationURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping-PagerDuty/"+version.Version)

	p.logger.Debugf("Sending PagerDuty request: %s", req.URL.String())

	// Send request
	resp, err := p.client.Do(req)
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
