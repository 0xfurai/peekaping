package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"

	"go.uber.org/zap"
)

type DiscordConfig struct {
	WebhookURL          string `json:"webhook_url" validate:"required,url"`
	BotDisplayName      string `json:"bot_display_name"`
	CustomMessagePrefix string `json:"custom_message_prefix"`
	MessageType         string `json:"message_type" validate:"omitempty,oneof=send_to_channel send_to_new_forum_post send_to_thread"`
	ThreadName          string `json:"thread_name"`
	ThreadID            string `json:"thread_id"`
}

type DiscordSender struct {
	logger *zap.SugaredLogger
}

// NewDiscordSender creates a DiscordSender
func NewDiscordSender(logger *zap.SugaredLogger) *DiscordSender {
	return &DiscordSender{logger: logger}
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

	s.logger.Infof("Sending Discord message: %s", message)

	finalMessage := message

	// Add custom message prefix if provided
	if cfg.CustomMessagePrefix != "" {
		finalMessage = cfg.CustomMessagePrefix + " " + finalMessage
	}

	// Prepare Discord webhook payload
	var payload map[string]interface{}

	if cfg.MessageType == "send_to_new_forum_post" {
		payload = map[string]interface{}{
			"content":     finalMessage,
			"thread_name": cfg.ThreadName,
		}
	} else {
		payload = map[string]interface{}{
			"content": finalMessage,
		}
	}

	if cfg.MessageType == "send_to_thread" {
		if cfg.ThreadID != "" {
			parsedURL, err := url.Parse(cfg.WebhookURL)
			if err == nil {
				q := parsedURL.Query()
				q.Set("thread_id", cfg.ThreadID)
				parsedURL.RawQuery = q.Encode()
				cfg.WebhookURL = parsedURL.String()
			} else {
				return fmt.Errorf("invalid webhook URL: %w", err)
			}
		}
	}

	// Add bot display name if provided
	if cfg.BotDisplayName != "" {
		payload["username"] = cfg.BotDisplayName
	}

	// Convert payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Discord payload: %w", err)
	}

	// Create HTTP request
	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.WebhookURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	s.logger.Debugf("Sending Discord webhook: %s", string(jsonPayload))

	// Send the request
	resp, err := client.Do(req)
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
