package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"peekaping/src/modules/shared"
	"peekaping/src/version"
	"time"

	"go.uber.org/zap"
)

type WeComConfig struct {
	WebhookURL string `json:"webhook_url" validate:"required,url"`
}

type WeComSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewWeComSender creates a new WeComSender instance
func NewWeComSender(logger *zap.SugaredLogger) *WeComSender {
	return &WeComSender{
		logger: logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (w *WeComSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[WeComConfig](configJSON)
}

func (w *WeComSender) Validate(configJSON string) error {
	cfg, err := w.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*WeComConfig))
}

func (w *WeComSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	monitor *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	// Unmarshal the configuration JSON into WeComConfig struct
	cfgAny, err := w.Unmarshal(configJSON)
	if err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	cfg := cfgAny.(*WeComConfig)

	w.logger.Infof("Sending WeCom notification to webhook: %s", cfg.WebhookURL)

	var content string
	// Format message content differently based on whether monitor/healthbeat info is available
	if monitor != nil && heartbeat != nil {
		var statusEmoji string
		// Select emoji based on monitor status
		switch heartbeat.Status {
		case shared.MonitorStatusUp:
			statusEmoji = "✅"
		case shared.MonitorStatusDown:
			statusEmoji = "❌"
		default:
			statusEmoji = "⚠️"
		}
		// Format message with status, monitor name, and timestamp
		content = fmt.Sprintf("%s **%s**\n> %s\n\nTime: %s",
			statusEmoji,
			monitor.Name,
			message,
			heartbeat.Time.Format("2006-01-02 15:04:05"))
	} else {
		content = message
	}

	// WeCom message format(markdown)
	// TODO: Add more formats if needed
	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]interface{}{
			"content": content,
		},
	}

	// Marshal payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create HTTP POST request with JSON payload
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.WebhookURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set request headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping-WeCom/"+version.Version)

	// Send HTTP request
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check for successful response status
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("WeCom API returned status %d", resp.StatusCode)
	}

	w.logger.Infof("WeCom notification sent successfully")
	return nil
}
