package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/version"

	"go.uber.org/zap"
)

// PagerDutyConfig holds the configuration for PagerDuty notifications
type PagerDutyConfig struct {
	IntegrationKey string `json:"pagerduty_integration_key" validate:"required" secret:"true"`
	IntegrationURL string `json:"pagerduty_integration_url" validate:"required,url" endpoint:"true" urlCredentials:"true" target:"url"`
	Priority       string `json:"pagerduty_priority"`
	AutoResolve    string `json:"pagerduty_auto_resolve"`
}

// PagerDutySender handles sending notifications to PagerDuty
type PagerDutySender struct {
	logger    *zap.SugaredLogger
	client    *http.Client
	publicURL PublicURLSource
}

// NewPagerDutySender creates a new PagerDutySender
func NewPagerDutySender(logger *zap.SugaredLogger, publicURL PublicURLSource, client *http.Client) *PagerDutySender {
	return &PagerDutySender{logger: logger, publicURL: publicURL, client: client}
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

// getSource returns the source field of the alert: what the monitor checks, or its name. A
// notification with no monitor has Peekaping as its source, since PagerDuty needs one.
func (p *PagerDutySender) getSource(monitor *monitor.Model) string {
	if source := p.getMonitorURL(monitor); source != "" {
		return source
	}
	if monitor == nil {
		return "Peekaping"
	}
	return monitor.Name
}

// buildPayload is the event to send, without the link back to Peekaping
func (p *PagerDutySender) buildPayload(message string, monitor *monitor.Model, title, eventAction string, cfg *PagerDutyConfig) map[string]any {
	monitorName := unknownMonitorName
	if monitor != nil {
		monitorName = monitor.Name
	}

	payload := map[string]any{
		"payload": map[string]any{
			"summary":  fmt.Sprintf("[%s] [%s] %s", title, monitorName, message),
			"severity": p.getSeverity(cfg),
			"source":   p.getSource(monitor),
		},
		"routing_key":  cfg.IntegrationKey,
		"event_action": eventAction,
	}
	// Alerts of one monitor, or one probe, are one incident. With no monitor, PagerDuty makes up a
	// key for a trigger, so each is an incident of its own.
	if monitor != nil {
		payload["dedup_key"] = fmt.Sprintf("Peekaping/%s", monitor.ID)
	}
	return payload
}

// post sends the payload to the integration URL
func (p *PagerDutySender) post(ctx context.Context, cfg *PagerDutyConfig, payload map[string]any) error {
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

	p.logger.Debugf("Sending PagerDuty request to: %s", logHost(cfg.IntegrationURL))

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
	return nil
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

	p.logger.Infof("Sending PagerDuty notification to: %s", logHost(cfg.IntegrationURL))

	// Determine event action
	eventAction := p.getEventAction(monitor, heartbeat, cfg)
	if eventAction == "" {
		p.logger.Infof("No action required for PagerDuty notification")
		return nil
	}

	// Generate title
	title := p.getTitle(heartbeat)

	// Prepare PagerDuty payload
	payload := p.buildPayload(message, monitor, title, eventAction, cfg)

	// Add client information if base URL is available
	if link := peekapingLink(publicURL(ctx, p.publicURL), monitor); link != "" {
		payload["client"] = "Peekaping"
		payload["client_url"] = link
	}

	if err := p.post(ctx, cfg, payload); err != nil {
		return err
	}

	p.logger.Infof("PagerDuty notification sent successfully")
	return nil
}
