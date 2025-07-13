package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"peekaping/src/config"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"peekaping/src/modules/shared"
	"peekaping/src/version"
	"strings"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

type SlackConfig struct {
	WebhookURL    string `json:"slack_webhook_url" validate:"required,url"`
	Username      string `json:"slack_username"`
	IconEmoji     string `json:"slack_icon_emoji"`
	Channel       string `json:"slack_channel"`
	RichMessage   bool   `json:"slack_rich_message"`
	ChannelNotify bool   `json:"slack_channel_notify"`
	UseTemplate   bool   `json:"use_template"`
	Template      string `json:"template"`
}

type SlackSender struct {
	logger *zap.SugaredLogger
	config *config.Config
}

// NewSlackSender creates a SlackSender
func NewSlackSender(logger *zap.SugaredLogger, config *config.Config) *SlackSender {
	return &SlackSender{logger: logger, config: config}
}

func (s *SlackSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[SlackConfig](configJSON)
}

func (s *SlackSender) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*SlackConfig))
}

// extractAddress extracts the URL from monitor for "Visit site" button
func (s *SlackSender) extractAddress(monitor *monitor.Model) string {
	if monitor == nil {
		return ""
	}

	// Try to extract URL from monitor config JSON
	// Since monitor.Config contains JSON configuration, we would need to parse it
	// For now, return empty string as the config structure isn't defined
	// This can be enhanced later based on the actual config structure
	return ""
}

// buildActions creates action buttons for the Slack message
func (s *SlackSender) buildActions(baseURL string, monitor *monitor.Model) []map[string]any {
	actions := []map[string]any{}

	// Add "Visit" button if base URL is available
	if baseURL != "" && monitor != nil {
		monitorURL := fmt.Sprintf("%s/monitors/%s", strings.TrimRight(baseURL, "/"), monitor.ID)
		actions = append(actions, map[string]any{
			"type": "button",
			"text": map[string]any{
				"type": "plain_text",
				"text": "Visit Peekaping",
			},
			"value": "Peekaping",
			"url":   monitorURL,
		})
	}

	// Add "Visit site" button if monitor has a valid address
	address := s.extractAddress(monitor)
	if address != "" {
		if _, err := url.Parse(address); err == nil {
			actions = append(actions, map[string]any{
				"type": "button",
				"text": map[string]any{
					"type": "plain_text",
					"text": "Visit site",
				},
				"value": "Site",
				"url":   address,
			})
		}
	}

	return actions
}

// buildBlocks creates the block structure for rich Slack messages
func (s *SlackSender) buildBlocks(baseURL string, monitor *monitor.Model, heartbeat *heartbeat.Model, title string, msg string) []map[string]any {
	blocks := []map[string]any{}

	// Header block
	blocks = append(blocks, map[string]any{
		"type": "header",
		"text": map[string]any{
			"type": "plain_text",
			"text": title,
		},
	})

	// Section block with message and time
	fields := []map[string]any{
		{
			"type": "mrkdwn",
			"text": "*Message*\n" + msg,
		},
	}

	// Add time field if heartbeat is available
	if heartbeat != nil {
		timeText := fmt.Sprintf("*Time*\n%s", heartbeat.Time.Format("2006-01-02 15:04:05"))
		fields = append(fields, map[string]any{
			"type": "mrkdwn",
			"text": timeText,
		})
	}

	blocks = append(blocks, map[string]any{
		"type":   "section",
		"fields": fields,
	})

	// Actions block with buttons
	actions := s.buildActions(baseURL, monitor)
	if len(actions) > 0 {
		blocks = append(blocks, map[string]any{
			"type":     "actions",
			"elements": actions,
		})
	}

	return blocks
}

// getStatusColor returns the appropriate color for the status
func (s *SlackSender) getStatusColor(status heartbeat.MonitorStatus) string {
	switch status {
	case shared.MonitorStatusDown:
		return "#e01e5a"
	case shared.MonitorStatusUp:
		return "#2eb886"
	case shared.MonitorStatusPending:
		return "#daa038"
	case shared.MonitorStatusMaintenance:
		return "#808080"
	default:
		return "#808080"
	}
}

func (s *SlackSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	monitor *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	cfg := cfgAny.(*SlackConfig)

	// Debug logging
	jsonDebug, _ := json.MarshalIndent(cfg, "", "  ")
	s.logger.Debugf("Slack config: %s", string(jsonDebug))
	s.logger.Infof("Sending Slack message to webhook: %s", cfg.WebhookURL)

	// Prepare template bindings
	bindings := PrepareTemplateBindings(monitor, heartbeat, message)

	// Debug template bindings
	if s.logger != nil {
		jsonDebug, _ := json.MarshalIndent(bindings, "", "  ")
		s.logger.Debugf("Template bindings: %s", string(jsonDebug))
	}

	// Prepare message text
	messageText := message
	if cfg.UseTemplate && cfg.Template != "" {
		engine := liquid.NewEngine()
		if rendered, err := engine.ParseAndRenderString(cfg.Template, bindings); err == nil {
			messageText = rendered
		} else {
			return fmt.Errorf("failed to render template: %w", err)
		}
	}

	// Add channel notification
	if cfg.ChannelNotify {
		messageText += " <!channel>"
	}

	// Prepare Slack payload
	payload := map[string]any{
		"text": messageText,
	}

	// Set optional parameters
	if cfg.Username != "" {
		payload["username"] = cfg.Username
	}

	if cfg.IconEmoji != "" {
		payload["icon_emoji"] = cfg.IconEmoji
	}

	if cfg.Channel != "" {
		payload["channel"] = cfg.Channel
	}

	// Handle rich message format
	if cfg.RichMessage && heartbeat != nil {
		title := "Peekaping Alert"

		// Use blocks for modern Slack message format
		blocks := s.buildBlocks(s.config.ClientURL, monitor, heartbeat, title, messageText)

		attachment := map[string]any{
			"color":  s.getStatusColor(heartbeat.Status),
			"blocks": blocks,
		}

		payload["attachments"] = []map[string]any{attachment}
		payload["text"] = title // Fallback text for notifications
	}

	// Convert payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Slack payload: %w", err)
	}

	s.logger.Debugf("Slack payload: %s", string(jsonPayload))

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.WebhookURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping-Slack/"+version.Version)

	s.logger.Debugf("Sending Slack webhook request: %s", req.URL.String())

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Slack webhook: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Slack webhook returned status: %s", resp.Status)
	}

	s.logger.Infof("Slack message sent successfully")
	return nil
}
