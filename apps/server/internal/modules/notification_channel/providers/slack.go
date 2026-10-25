package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"peekaping/internal/config"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/version"
	"regexp"
	"strings"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

type SlackConfig struct {
	WebhookURL    string `json:"slack_webhook_url" validate:"omitempty,url" secret:"true" endpoint:"true"`
	BotToken      string `json:"slack_bot_token" secret:"true"`
	Username      string `json:"slack_username"`
	IconEmoji     string `json:"slack_icon_emoji"`
	Channel       string `json:"slack_channel"`
	RichMessage   bool   `json:"slack_rich_message"`
	ChannelNotify bool   `json:"slack_channel_notify"`
	UseTemplate   bool   `json:"use_template"`
	Template      string `json:"template"`
}

// slackPostMessageURL is the Web API endpoint used when a bot token is configured
const slackPostMessageURL = "https://slack.com/api/chat.postMessage"

// slackErrorCode matches the reasons Slack webhooks return, such as no_service
var slackErrorCode = regexp.MustCompile(`^[a-z_]{1,64}$`)

type SlackSender struct {
	logger *zap.SugaredLogger
	config *config.Config
	client *http.Client
	apiURL string
}

// NewSlackSender creates a SlackSender
func NewSlackSender(logger *zap.SugaredLogger, config *config.Config, client *http.Client) *SlackSender {
	return &SlackSender{
		logger: logger,
		config: config,
		client: client,
		apiURL: slackPostMessageURL,
	}
}

func (s *SlackSender) Unmarshal(configJSON string) (any, error) {
	cfg, err := GenericUnmarshal[SlackConfig](configJSON)
	if err != nil {
		return nil, err
	}
	// Pasted tokens often end with a newline, which net/http rejects in a header
	cfg.BotToken = strings.TrimSpace(cfg.BotToken)
	cfg.Channel = strings.TrimSpace(cfg.Channel)
	return cfg, nil
}

func (s *SlackSender) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	slackCfg := cfg.(*SlackConfig)

	// Ensure either WebhookURL or BotToken is provided
	if slackCfg.WebhookURL == "" && slackCfg.BotToken == "" {
		return fmt.Errorf("either slack_webhook_url or slack_bot_token must be provided")
	}

	// If using BotToken, Channel is required
	if slackCfg.BotToken != "" && slackCfg.Channel == "" {
		return fmt.Errorf("slack_channel is required when using slack_bot_token")
	}

	return GenericValidator(slackCfg)
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
	if link := peekapingLink(baseURL, monitor); link != "" {
		actions = append(actions, map[string]any{
			"type": "button",
			"text": map[string]any{
				"type": "plain_text",
				"text": "Visit Peekaping",
			},
			"value": "Peekaping",
			"url":   link,
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

	// Prepare template bindings
	bindings := PrepareTemplateBindings(monitor, heartbeat, message)

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

	// Handle rich message format. A probe's notification carries no heartbeat, but has a button
	// to the Probes page.
	if cfg.RichMessage && (heartbeat != nil || monitor.IsProbeEvent()) {
		title := "Peekaping Alert"

		// Use blocks for modern Slack message format
		blocks := s.buildBlocks(s.config.ClientURL, monitor, heartbeat, title, messageText)

		// Without a heartbeat there is no status to color the message by, so it is grey
		color := "#808080"
		if heartbeat != nil {
			color = s.getStatusColor(heartbeat.Status)
		}
		attachment := map[string]any{
			"color":  color,
			"blocks": blocks,
		}

		payload["attachments"] = []map[string]any{attachment}
		// Fallback text for notifications. A probe's notification keeps its message there, so a
		// phone's notification still says which probe went offline or came back.
		if !monitor.IsProbeEvent() {
			payload["text"] = title
		}
	}

	// Convert payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Slack payload: %w", err)
	}

	s.logger.Debugf("Slack payload: %s", string(jsonPayload))

	// Use Slack API if BotToken is provided, otherwise use webhook.
	// The webhook URL is a secret, so it is never logged.
	targetURL, target := cfg.WebhookURL, "webhook"
	if cfg.BotToken != "" {
		targetURL, target = s.apiURL, "API"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping-Slack/"+version.Version)
	if cfg.BotToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.BotToken)
		s.logger.Infof("Sending Slack message via API to channel: %s", cfg.Channel)
	} else {
		s.logger.Infof("Sending Slack message via webhook")
	}

	resp, err := s.client.Do(req)
	if err != nil {
		// url.Error repeats the request URL, which would leak the webhook URL
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("failed to send Slack message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		reason := strings.TrimSpace(string(body))
		// Another endpoint could echo the secret webhook URL in its body or reason phrase,
		// so only Slack's error codes and the standard status text are kept
		status := strings.TrimSpace(fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode)))
		if cfg.BotToken == "" && !slackErrorCode.MatchString(reason) {
			return fmt.Errorf("Slack webhook returned status %s", status)
		}
		return fmt.Errorf("Slack %s returned status %s: %s", target, status, reason)
	}

	if cfg.BotToken != "" {
		// chat.postMessage reports failures as HTTP 200 with ok=false
		var apiResp struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
			return fmt.Errorf("failed to decode Slack API response: %w", err)
		}
		if !apiResp.OK {
			return fmt.Errorf("Slack API error: %s", apiResp.Error)
		}
	}

	s.logger.Infof("Slack message sent successfully")
	return nil
}
