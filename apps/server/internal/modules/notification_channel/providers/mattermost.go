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
	"strings"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

type MattermostConfig struct {
	WebhookURL  string `json:"webhook_url" validate:"required,url" secret:"true" endpoint:"true" target:"url"`
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
func NewMattermostSender(logger *zap.SugaredLogger, client *http.Client) *MattermostSender {
	return &MattermostSender{
		logger: logger,
		client: client,
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
	payload := map[string]any{
		"username": mattermostUsername(cfg),
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

// mattermostUsername is the configured user name, or Peekaping
func mattermostUsername(cfg *MattermostConfig) string {
	if cfg.Username == "" {
		return "Peekaping"
	}
	return cfg.Username
}

// mattermostIconEmoji is the emoji for the notification. The configured emoji is a pair, the online
// one and the offline one, which go with an Up and a Down. Anything else is used as it is.
func mattermostIconEmoji(cfg *MattermostConfig, heartbeat *heartbeat.Model) string {
	emojis := strings.Split(cfg.IconEmoji, " ")
	if heartbeat == nil || len(emojis) < 2 {
		return cfg.IconEmoji
	}
	iconEmojiOnline, iconEmojiOffline := emojis[0], emojis[1]

	switch heartbeat.Status {
	case shared.MonitorStatusDown:
		if iconEmojiOffline != "" {
			return iconEmojiOffline
		}
	case shared.MonitorStatusUp:
		if iconEmojiOnline != "" {
			return iconEmojiOnline
		}
	}
	return cfg.IconEmoji
}

// mattermostStatus is what the heartbeat says about the status: the field that shows it, if it
// has one, the word for it and the color of the attachment
func mattermostStatus(heartbeat *heartbeat.Model) (statusField map[string]any, statusText, color string) {
	if heartbeat == nil {
		return nil, "unknown", "#000000"
	}
	switch heartbeat.Status {
	case shared.MonitorStatusDown:
		return map[string]any{
			"short": false,
			"title": "Error",
			"value": heartbeat.Msg,
		}, "down", "#FF0000"
	case shared.MonitorStatusUp:
		return map[string]any{
			"short": false,
			"title": "Ping",
			"value": fmt.Sprintf("%dms", heartbeat.Ping),
		}, "up", "#32CD32"
	}
	return nil, "unknown", "#000000"
}

// mattermostTitleLink is the URL in the monitor's config, if it has one
func mattermostTitleLink(monitor *monitor.Model) string {
	if monitor.Config == "" {
		return ""
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(monitor.Config), &config); err != nil {
		return ""
	}
	url, _ := config["url"].(string)
	return url
}

// mattermostFields are the fields of the attachment: the status, if it has a field, and the time of
// the heartbeat
func mattermostFields(statusField map[string]any, heartbeat *heartbeat.Model) []map[string]any {
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
	return fields
}

// addMattermostMonitor adds to the attachment the title that names the monitor and its status, and
// a link to the monitor's URL
func addMattermostMonitor(attachment map[string]any, monitor *monitor.Model, statusText string) {
	monitorName := monitor.Name
	if monitorName == "" {
		monitorName = "Monitor"
	}

	attachment["fallback"] = fmt.Sprintf("Your %s service went %s", monitorName, statusText)
	attachment["title"] = fmt.Sprintf("%s service went %s", monitorName, statusText)

	// Add title_link if we can extract URL from monitor
	if url := mattermostTitleLink(monitor); url != "" {
		attachment["title_link"] = url
	}
}

func (m *MattermostSender) buildRichMessage(cfg *MattermostConfig, message string, monitor *monitor.Model, heartbeat *heartbeat.Model) map[string]any {
	username := mattermostUsername(cfg)
	if monitor != nil && monitor.Name != "" {
		username = monitor.Name + " " + username
	}

	// Determine status information
	statusField, statusText, color := mattermostStatus(heartbeat)

	// Build the payload
	payload := map[string]any{
		"username": username,
	}

	if cfg.Channel != "" {
		payload["channel"] = strings.ToLower(cfg.Channel)
	}

	if iconEmoji := mattermostIconEmoji(cfg, heartbeat); iconEmoji != "" {
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
		addMattermostMonitor(attachment, monitor, statusText)
	}

	// Add fields
	if fields := mattermostFields(statusField, heartbeat); len(fields) > 0 {
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
