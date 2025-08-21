package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"peekaping/src/modules/shared"

	"go.uber.org/zap"
)

type PagerTreeConfig struct {
	IntegrationURL string `json:"integrationUrl" validate:"required,url"`
	Urgency        string `json:"urgency" validate:"omitempty,oneof=silent low medium high critical"`
	AutoResolve    bool   `json:"autoResolve"`
	AuthToken      string `json:"authToken" validate:"omitempty"`
}

type PagerTreePayload struct {
	EventType   string                 `json:"event_type"`
	ID          string                 `json:"Id"`
	Title       string                 `json:"Title"`
	Description string                 `json:"Description"`
	Urgency     string                 `json:"urgency,omitempty"`
	Tags        []string               `json:"Tags,omitempty"`
	Meta        map[string]interface{} `json:"Meta,omitempty"`
}

type PagerTreeSender struct {
	logger *zap.SugaredLogger
}

func NewPagerTreeSender(logger *zap.SugaredLogger) *PagerTreeSender {
	return &PagerTreeSender{
		logger: logger,
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

	// Determine event type based on monitor status
	eventType := "create"
	if hb != nil && hb.Status == shared.MonitorStatusUp && cfg.AutoResolve {
		eventType = "resolve"
	} else if hb != nil && hb.Status != shared.MonitorStatusDown {
		// Only send notifications for DOWN status or UP (if auto-resolve is enabled)
		p.logger.Debugf("Skipping PagerTree notification for non-critical status: %d", int(hb.Status))
		return nil
	}

	// Build the payload
	payload := PagerTreePayload{
		EventType: eventType,
		ID:        fmt.Sprintf("monitor-%s-%d", mon.ID, time.Now().Unix()),
		Urgency:   cfg.Urgency,
	}

	// Prepare template bindings (kept for potential future use)
	_ = PrepareTemplateBindings(mon, hb, message)

	// Set title based on status
	if hb != nil {
		statusText := "DOWN"
		if hb.Status == shared.MonitorStatusUp {
			statusText = "UP"
		}
		payload.Title = fmt.Sprintf("Monitor %s is %s", mon.Name, statusText)
	} else {
		payload.Title = fmt.Sprintf("Monitor %s Alert", mon.Name)
	}

	// Set description
	if message != "" {
		payload.Description = message
	} else if hb != nil && hb.Msg != "" {
		payload.Description = hb.Msg
	} else {
		payload.Description = fmt.Sprintf("Monitor %s status changed", mon.Name)
	}

	// Add timestamp to description
	if hb != nil {
		payload.Description += fmt.Sprintf(" - Last checked: %s", hb.Time.Format(time.RFC3339))
	}

	// Add tags
	payload.Tags = []string{
		"peekaping",
		fmt.Sprintf("monitor-type:%s", mon.Type),
	}

	// Add metadata
	payload.Meta = map[string]interface{}{
		"monitor_id":   mon.ID,
		"monitor_name": mon.Name,
		"monitor_type": mon.Type,
	}

	// Parse monitor config to get URL if available
	if mon.Config != "" {
		var config map[string]interface{}
		if err := json.Unmarshal([]byte(mon.Config), &config); err == nil {
			if url, ok := config["url"].(string); ok {
				payload.Meta["monitor_url"] = url
			}
		}
	}

	if hb != nil {
		payload.Meta["status"] = int(hb.Status)
		if hb.Ping > 0 {
			payload.Meta["response_time"] = fmt.Sprintf("%dms", hb.Ping)
		}
		if hb.Duration > 0 {
			payload.Meta["duration"] = fmt.Sprintf("%dms", hb.Duration)
		}
	}

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
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
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

	p.logger.Infof("PagerTree notification sent successfully - event: %s, monitor: %s (%s)",
		eventType, mon.Name, mon.ID)

	return nil
}