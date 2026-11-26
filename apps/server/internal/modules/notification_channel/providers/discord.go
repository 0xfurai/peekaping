package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"time"

	"go.uber.org/zap"
)

type DiscordConfig struct {
	WebhookURL          string `json:"webhook_url" validate:"required,url" secret:"true" endpoint:"true" target:"url"`
	BotDisplayName      string `json:"bot_display_name"`
	CustomMessagePrefix string `json:"custom_message_prefix"`
	MessageType         string `json:"message_type" validate:"omitempty,oneof=send_to_channel send_to_new_forum_post send_to_thread"`
	ThreadName          string `json:"thread_name"`
	ThreadID            string `json:"thread_id"`
}

type DiscordSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewDiscordSender creates a DiscordSender
func NewDiscordSender(logger *zap.SugaredLogger, client *http.Client) *DiscordSender {
	return &DiscordSender{logger: logger, client: client}
}

func (s *DiscordSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[DiscordConfig](configJSON)
}

func (s *DiscordSender) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*DiscordConfig))
}

func (s *DiscordSender) Send(
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
	cfg := cfgAny.(*DiscordConfig)

	bindings := PrepareTemplateBindings(monitor, heartbeat, message)

	s.logger.Infof("Sending Discord message: %s", message)

	finalEmbed := createDiscordEmbed(bindings, heartbeat != nil)

	s.logger.Debug(finalEmbed)

	webhookURL, err := discordWebhookURL(cfg)
	if err != nil {
		return err
	}

	payload := discordPayload(cfg, finalEmbed)

	// Convert payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Discord payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	s.logger.Debugf("Sending Discord webhook: %s", string(jsonPayload))

	// Send the request
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Discord webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Discord webhook returned status: %s", resp.Status)
	}

	s.logger.Infof("Discord message sent successfully")
	return nil
}

// discordPayload builds the webhook payload: the custom message prefix as the content, the embed,
// the bot's display name if one is set, and for a new forum post the name of its thread
func discordPayload(cfg *DiscordConfig, embed map[string]interface{}) map[string]interface{} {
	payload := map[string]interface{}{
		"content":     cfg.CustomMessagePrefix,
		"embeds":      [1]map[string]interface{}{embed},
		"attachments": []*string{},
	}
	if cfg.MessageType == "send_to_new_forum_post" {
		payload["thread_name"] = cfg.ThreadName
	}
	if cfg.BotDisplayName != "" {
		payload["username"] = cfg.BotDisplayName
	}
	return payload
}

// discordWebhookURL returns the URL to post to: the webhook URL, with the thread ID in its query
// for a message sent to a thread
func discordWebhookURL(cfg *DiscordConfig) (string, error) {
	if cfg.MessageType != "send_to_thread" || cfg.ThreadID == "" {
		return cfg.WebhookURL, nil
	}
	parsedURL, err := url.Parse(cfg.WebhookURL)
	if err != nil {
		return "", fmt.Errorf("invalid webhook URL: %w", err)
	}
	q := parsedURL.Query()
	q.Set("thread_id", cfg.ThreadID)
	parsedURL.RawQuery = q.Encode()
	return parsedURL.String(), nil
}

func createDiscordEmbed(bindings map[string]any, hasHeartbeat bool) map[string]interface{} {
	var fields []*map[string]interface{}

	fields = append(fields, &map[string]interface{}{
		"name":   "Name",
		"value":  bindings["name"],
		"inline": true,
	})
	// Certificate expiry notifications carry no heartbeat, so they have no status, ping or time
	if hasHeartbeat {
		fields = append(fields, &map[string]interface{}{
			"name":   "Status",
			"value":  bindings["status"],
			"inline": true,
		})
		if bindings["ping"] != 0 {
			fields = append(fields, &map[string]interface{}{
				"name":   "Ping",
				"value":  fmt.Sprintf("%d ms", bindings["ping"]),
				"inline": true,
			})
		}
		if t := bindings["time"].(time.Time); !t.IsZero() {
			fields = append(fields, &map[string]interface{}{
				"name":  "Time",
				"value": t.Format("2006-01-02 15:04:05"),
			})
		}
	}
	fields = append(fields, &map[string]interface{}{
		"name":  "Message",
		"value": bindings["msg"],
	})

	if !hasHeartbeat {
		return map[string]interface{}{
			"title": bindings["name"],
			// Pending's yellow, as a warning
			"color":  humanReadableStatusColor(int(shared.MonitorStatusPending)),
			"fields": fields,
		}
	}

	return map[string]interface{}{
		"title":  fmt.Sprintf("%s Your service %s is %s %s", bindings["status_icon"], bindings["name"], bindings["status"], bindings["status_icon"]),
		"color":  bindings["status_color"],
		"fields": fields,
	}
}
