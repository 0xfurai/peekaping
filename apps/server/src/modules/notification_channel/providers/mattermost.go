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
	"strings"
	"time"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

type MattermostConfig struct {
	WebhookURL  string `json:"webhook_url" validate:"required,url"`
	Username    string `json:"username"`
	Channel     string `json:"channel"`
	IconURL     string `json:"icon_url"`
	IconEmoji   string `json:"icon_emoji"`
	UseTemplate bool   `json:"use_template"`
	Template    string `json:"template"`
}

type MattermostSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewMattermostSender creates a MattermostSender
func NewMattermostSender(logger *zap.SugaredLogger) *MattermostSender {
	return &MattermostSender{
		logger: logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (m *MattermostSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[MattermostConfig](configJSON)
}

func (m *MattermostSender) Validate(configJSON string) error {
	cfg, err := m.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*MattermostConfig))
}

func (m *MattermostSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	monitor *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := m.Unmarshal(configJSON)
	if err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	cfg := cfgAny.(*MattermostConfig)

	// If heartbeat is null, this is a test or certificate notification
	if heartbeat == nil {
		return m.sendTestMessage(ctx, cfg, message)
	}

	// Check if template should be used
	if cfg.UseTemplate && cfg.Template != "" {
		// Prepare template bindings
		bindings := PrepareTemplateBindings(monitor, heartbeat, message)

		// Render the template
		engine := liquid.NewEngine()
		renderedMessage, err := engine.ParseAndRenderString(cfg.Template, bindings)
		if err != nil {
			return fmt.Errorf("failed to render template: %w", err)
		}

		// Send simple text message with rendered template
		return m.sendSimpleMessage(ctx, cfg, renderedMessage)
	}

	// Create the rich message payload
	payload := m.buildRichMessage(cfg, message, monitor, heartbeat)

	// Send the message
	return m.sendMessage(ctx, cfg.WebhookURL, payload)
}

func (m *MattermostSender) sendTestMessage(ctx context.Context, cfg *MattermostConfig, message string) error {
	// Check if template should be used for test messages
	if cfg.UseTemplate && cfg.Template != "" {
		// Prepare template bindings for test message (no monitor/heartbeat)
		bindings := PrepareTemplateBindings(nil, nil, message)

		// Render the template
		engine := liquid.NewEngine()
		renderedMessage, err := engine.ParseAndRenderString(cfg.Template, bindings)
		if err != nil {
			return fmt.Errorf("failed to render template: %w", err)
		}

		// Send simple text message with rendered template
		return m.sendSimpleMessage(ctx, cfg, renderedMessage)
	}

	// Default test message behavior
	return m.sendSimpleMessage(ctx, cfg, message)
}

func (m *MattermostSender) sendSimpleMessage(ctx context.Context, cfg *MattermostConfig, message string) error {
	username := cfg.Username
	if username == "" {
		username = "Peekaping"
	}

	payload := map[string]any{
		"username": username,
		"text":     message,
	}

	if cfg.Channel != "" {
		payload["channel"] = strings.ToLower(cfg.Channel)
	}

	if cfg.IconURL != "" {
		payload["icon_url"] = cfg.IconURL
	}

	if cfg.IconEmoji != "" {
		payload["icon_emoji"] = cfg.IconEmoji
	}

	return m.sendMessage(ctx, cfg.WebhookURL, payload)
}

func (m *MattermostSender) buildRichMessage(cfg *MattermostConfig, message string, monitor *monitor.Model, heartbeat *heartbeat.Model) map[string]any {
	username := cfg.Username
	if username == "" {
		username = "Peekaping"
	}

	if monitor != nil && monitor.Name != "" {
		username = monitor.Name + " " + username
	}

	// Extract emoji configuration
	iconEmojiOnline := ""
	iconEmojiOffline := ""
	iconEmoji := cfg.IconEmoji

	if cfg.IconEmoji != "" {
		emojiArray := strings.Split(cfg.IconEmoji, " ")
		if len(emojiArray) >= 2 {
			iconEmojiOnline = emojiArray[0]
			iconEmojiOffline = emojiArray[1]
		}
	}

	// Determine status information
	var statusField map[string]any
	statusText := "unknown"
	color := "#000000"

	if heartbeat != nil {
		switch heartbeat.Status {
		case shared.MonitorStatusDown:
			if iconEmojiOffline != "" {
				iconEmoji = iconEmojiOffline
			}
			statusField = map[string]any{
				"short": false,
				"title": "Error",
				"value": heartbeat.Msg,
			}
			statusText = "down"
			color = "#FF0000"
		case shared.MonitorStatusUp:
			if iconEmojiOnline != "" {
				iconEmoji = iconEmojiOnline
			}
			statusField = map[string]any{
				"short": false,
				"title": "Ping",
				"value": fmt.Sprintf("%dms", heartbeat.Ping),
			}
			statusText = "up"
			color = "#32CD32"
		}
	}

	// Build the payload
	payload := map[string]any{
		"username": username,
	}

	if cfg.Channel != "" {
		payload["channel"] = strings.ToLower(cfg.Channel)
	}

	if iconEmoji != "" {
		payload["icon_emoji"] = iconEmoji
	}

	if cfg.IconURL != "" {
		payload["icon_url"] = cfg.IconURL
	}

	// Build attachment
	attachment := map[string]any{
		"color": color,
	}

	if monitor != nil {
		monitorName := monitor.Name
		if monitorName == "" {
			monitorName = "Monitor"
		}

		attachment["fallback"] = fmt.Sprintf("Your %s service went %s", monitorName, statusText)
		attachment["title"] = fmt.Sprintf("%s service went %s", monitorName, statusText)

		// Add title_link if we can extract URL from monitor
		if monitor.Config != "" {
			var config map[string]any
			if err := json.Unmarshal([]byte(monitor.Config), &config); err == nil {
				if url, ok := config["url"].(string); ok && url != "" {
					attachment["title_link"] = url
				}
			}
		}
	}

	// Add fields
	fields := []map[string]any{}
	if statusField != nil {
		fields = append(fields, statusField)
	}

	if heartbeat != nil {
		// Add time field
		timeField := map[string]any{
			"short": true,
			"title": "Time",
			"value": heartbeat.Time.Format("2006-01-02 15:04:05"),
		}
		fields = append(fields, timeField)
	}

	if len(fields) > 0 {
		attachment["fields"] = fields
	}

	payload["attachments"] = []map[string]any{attachment}

	return payload
}

func (m *MattermostSender) sendMessage(ctx context.Context, webhookURL string, payload map[string]any) error {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping-Mattermost/"+version.Version)

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("Mattermost API returned status %d", resp.StatusCode)
	}

	m.logger.Infof("Mattermost notification sent successfully")
	return nil
}
