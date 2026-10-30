package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"go.uber.org/zap"
)

type PagerTreeConfig struct {
	IntegrationURL string `json:"integrationUrl" validate:"required,url" secret:"true" endpoint:"true" target:"url"`
	Urgency        string `json:"urgency" validate:"omitempty,oneof=silent low medium high critical"`
	AutoResolve    bool   `json:"autoResolve"`
	AuthToken      string `json:"authToken" validate:"omitempty" secret:"true"`
}

type PagerTreePayload struct {
	EventType   string                 `json:"event_type"`
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Urgency     string                 `json:"urgency,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	Meta        map[string]interface{} `json:"meta,omitempty"`
}

type PagerTreeSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

func NewPagerTreeSender(logger *zap.SugaredLogger, client *http.Client) *PagerTreeSender {
	return &PagerTreeSender{
		logger: logger,
		client: client,
	}
}

func (p *PagerTreeSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[PagerTreeConfig](configJSON)
}

func (p *PagerTreeSender) Validate(configJSON string) error {
	cfg, err := p.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*PagerTreeConfig))
}

// eventTypeForStatus is the PagerTree event type for a monitor, or a probe's stand-in, that has
// this status: Down (a probe offline) creates an alert, Up (a probe back online) resolves it if
// auto-resolve is on. It is "" when nothing is sent.
func eventTypeForStatus(status shared.MonitorStatus, autoResolve bool) string {
	switch {
	case status == shared.MonitorStatusDown:
		return "create"
	case status == shared.MonitorStatusUp && autoResolve:
		return "resolve"
	default:
		return ""
	}
}

// pagerTreeEventType is the event type of a notification, or "" when nothing is sent. A
// certificate-expiry warning, which has no heartbeat and is not a probe's, creates an alert.
func pagerTreeEventType(cfg *PagerTreeConfig, mon *monitor.Model, hb *heartbeat.Model, isProbeEvent bool) string {
	switch {
	case hb != nil:
		return eventTypeForStatus(hb.Status, cfg.AutoResolve)
	case isProbeEvent:
		return eventTypeForStatus(mon.Status, cfg.AutoResolve)
	default:
		return "create"
	}
}

// pagerTreeAlertID is the ID of the alert a notification creates or resolves
func pagerTreeAlertID(mon *monitor.Model, hb *heartbeat.Model, isProbeEvent bool) string {
	switch {
	case isProbeEvent:
		// Alerts of one probe are one alert: the same ID, in the form of PagerDuty's dedup key
		return fmt.Sprintf("Peekaping/%s", mon.ID)
	case hb == nil:
		// A certificate-expiry warning gets a new alert each time: PP-136 gives it its own identity
		return fmt.Sprintf("monitor-%s-%d", mon.ID, time.Now().Unix())
	default:
		// One ID per monitor, so its Up resolves the alert its Down created and a resent Down
		// updates the open alert
		return fmt.Sprintf("monitor-%s", mon.ID)
	}
}

// pagerTreeTitle is the title of the alert, set by the status
func pagerTreeTitle(mon *monitor.Model, hb *heartbeat.Model, isProbeEvent bool) string {
	if isProbeEvent {
		statusText := "offline"
		if mon.Status == shared.MonitorStatusUp {
			statusText = "online"
		}
		return fmt.Sprintf("%s is %s", mon.Name, statusText)
	}
	if hb != nil {
		statusText := "DOWN"
		if hb.Status == shared.MonitorStatusUp {
			statusText = "UP"
		}
		return fmt.Sprintf("Monitor %s is %s", mon.Name, statusText)
	}
	return fmt.Sprintf("Monitor %s Alert", mon.Name)
}

// pagerTreeDescription is the message, or the heartbeat's, or a default, followed by when the
// heartbeat was last checked
func pagerTreeDescription(message string, mon *monitor.Model, hb *heartbeat.Model) string {
	var description string
	if message != "" {
		description = message
	} else if hb != nil && hb.Msg != "" {
		description = hb.Msg
	} else {
		description = fmt.Sprintf("Monitor %s status changed", mon.Name)
	}

	// Add timestamp to description
	if hb != nil {
		description += fmt.Sprintf(" - Last checked: %s", hb.Time.Format(time.RFC3339))
	}
	return description
}

// pagerTreeMonitorURL is the URL in the monitor's config, if it has one
func pagerTreeMonitorURL(mon *monitor.Model) (string, bool) {
	if mon.Config == "" {
		return "", false
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(mon.Config), &config); err != nil {
		return "", false
	}
	url, ok := config["url"].(string)
	return url, ok
}

// pagerTreeMeta is the metadata of the alert: the monitor, and the heartbeat or the probe's status
func pagerTreeMeta(mon *monitor.Model, hb *heartbeat.Model, isProbeEvent bool) map[string]interface{} {
	meta := map[string]interface{}{
		"monitor_id":   mon.ID,
		"monitor_name": mon.Name,
		"monitor_type": mon.Type,
	}

	// Parse monitor config to get URL if available
	if url, ok := pagerTreeMonitorURL(mon); ok {
		meta["monitor_url"] = url
	}

	if hb != nil {
		meta["status"] = int(hb.Status)
		if hb.Ping > 0 {
			meta["response_time"] = fmt.Sprintf("%dms", hb.Ping)
		}
		if hb.Duration > 0 {
			meta["duration"] = fmt.Sprintf("%dms", hb.Duration)
		}
	} else if isProbeEvent {
		meta["status"] = int(mon.Status)
	}
	return meta
}

// post sends the payload to the integration URL
func (p *PagerTreeSender) post(ctx context.Context, cfg *PagerTreeConfig, payload PagerTreePayload) error {
	// Marshal payload to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.IntegrationURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if cfg.AuthToken != "" {
		req.Header.Set("pagertree-token", cfg.AuthToken)
	}

	// Send the request
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send PagerTree notification: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var respBody bytes.Buffer
		respBody.ReadFrom(resp.Body)
		return fmt.Errorf("PagerTree API returned status %d: %s", resp.StatusCode, respBody.String())
	}
	return nil
}

func (p *PagerTreeSender) Send(ctx context.Context, configJSON, message string, mon *monitor.Model, hb *heartbeat.Model) error {
	config, err := p.Unmarshal(configJSON)
	if err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	cfg := config.(*PagerTreeConfig)

	// Set default urgency if not specified
	if cfg.Urgency == "" {
		cfg.Urgency = "medium"
	}

	// A probe's notification has no heartbeat, so the status of its stand-in monitor says whether
	// the probe went offline or came back online
	isProbeEvent := hb == nil && mon.IsProbeEvent()

	// Determine event type based on monitor status
	eventType := pagerTreeEventType(cfg, mon, hb, isProbeEvent)
	if eventType == "" {
		// Only send notifications for DOWN status or UP (if auto-resolve is enabled)
		p.logger.Debugf("Skipping PagerTree notification for non-critical status")
		return nil
	}

	// A notification with no monitor is about an unknown one
	if mon == nil {
		mon = &monitor.Model{Name: unknownMonitorName}
	}

	// Prepare template bindings (kept for potential future use)
	_ = PrepareTemplateBindings(mon, hb, message)

	// Build the payload
	payload := PagerTreePayload{
		EventType:   eventType,
		ID:          pagerTreeAlertID(mon, hb, isProbeEvent),
		Urgency:     cfg.Urgency,
		Title:       pagerTreeTitle(mon, hb, isProbeEvent),
		Description: pagerTreeDescription(message, mon, hb),
		Tags: []string{
			"peekaping",
			fmt.Sprintf("monitor-type:%s", mon.Type),
		},
		Meta: pagerTreeMeta(mon, hb, isProbeEvent),
	}

	if err := p.post(ctx, cfg, payload); err != nil {
		return err
	}

	p.logger.Infof("PagerTree notification sent successfully - event: %s, monitor: %s (%s)",
		eventType, mon.Name, mon.ID)

	return nil
}
