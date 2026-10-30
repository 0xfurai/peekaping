package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	WebhookURL    string `json:"slack_webhook_url" validate:"omitempty,url" secret:"true" endpoint:"true" target:"url"`
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
	logger    *zap.SugaredLogger
	publicURL PublicURLSource
	client    *http.Client
	apiURL    string
}

// NewSlackSender creates a SlackSender
func NewSlackSender(logger *zap.SugaredLogger, publicURL PublicURLSource, client *http.Client) *SlackSender {
	return &SlackSender{
		logger:    logger,
		publicURL: publicURL,
		client:    client,
		apiURL:    slackPostMessageURL,
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

// slackMessageText is the message, rendered through the template when the channel has one, with the
// channel notification added when it is on
func slackMessageText(cfg *SlackConfig, message string, bindings map[string]any) (string, error) {
	messageText := message
	if cfg.UseTemplate && cfg.Template != "" {
		engine := liquid.NewEngine()
		if rendered, err := engine.ParseAndRenderString(cfg.Template, bindings); err == nil {
			messageText = rendered
		} else {
			return "", fmt.Errorf("failed to render template: %w", err)
		}
	}

	// Add channel notification
	if cfg.ChannelNotify {
		messageText += " <!channel>"
	}
	return messageText, nil
}

// buildPayload is the payload to post: the message text with the channel's options, or for a rich
// message, blocks in an attachment
func (s *SlackSender) buildPayload(ctx context.Context, cfg *SlackConfig, message string, m *monitor.Model, hb *heartbeat.Model) (map[string]any, error) {
	// Prepare template bindings
	bindings := PrepareTemplateBindings(m, hb, message)

	// Prepare message text
	messageText, err := slackMessageText(cfg, message, bindings)
	if err != nil {
		return nil, err
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
	if cfg.RichMessage && (hb != nil || m.IsProbeEvent()) {
		s.addRichMessage(ctx, payload, messageText, m, hb)
	}
	return payload, nil
}

// addRichMessage puts the message in blocks in an attachment, colored by the heartbeat's status
func (s *SlackSender) addRichMessage(ctx context.Context, payload map[string]any, messageText string, m *monitor.Model, hb *heartbeat.Model) {
	title := "Peekaping Alert"

	// Use blocks for modern Slack message format
	blocks := s.buildBlocks(publicURL(ctx, s.publicURL), m, hb, title, messageText)

	// Without a heartbeat there is no status to color the message by, so it is grey
	color := "#808080"
	if hb != nil {
		color = s.getStatusColor(hb.Status)
	}
	attachment := map[string]any{
		"color":  color,
		"blocks": blocks,
	}

	payload["attachments"] = []map[string]any{attachment}
	// Fallback text for notifications. A probe's notification keeps its message there, so a
	// phone's notification still says which probe went offline or came back.
	if !m.IsProbeEvent() {
		payload["text"] = title
	}
}

// newRequest makes the request that posts the payload: to the Web API when a bot token is
// configured, and otherwise to the webhook. The webhook URL is a secret, so it is never logged.
func (s *SlackSender) newRequest(ctx context.Context, cfg *SlackConfig, jsonPayload []byte) (*http.Request, error) {
	targetURL := cfg.WebhookURL
	if cfg.BotToken != "" {
		targetURL = s.apiURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping-Slack/"+version.Version)
	if cfg.BotToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.BotToken)
		s.logger.Infof("Sending Slack message via API to channel: %s", cfg.Channel)
	} else {
		s.logger.Infof("Sending Slack message via webhook")
	}
	return req, nil
}

// slackStatusError is the error for a response that is not a success. Another endpoint could echo
// the secret webhook URL in its body or reason phrase, so only Slack's error codes and the
// standard status text are kept.
func slackStatusError(resp *http.Response, cfg *SlackConfig) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	reason := strings.TrimSpace(string(body))
	status := strings.TrimSpace(fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode)))
	if cfg.BotToken == "" && !slackErrorCode.MatchString(reason) {
		return fmt.Errorf("Slack webhook returned status %s", status)
	}
	target := "webhook"
	if cfg.BotToken != "" {
		target = "API"
	}
	return fmt.Errorf("Slack %s returned status %s: %s", target, status, reason)
}

// checkSlackResponse is the error for a response that is not a success, or for a Web API response
// that says it failed
func checkSlackResponse(resp *http.Response, cfg *SlackConfig) error {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return slackStatusError(resp, cfg)
	}
	if cfg.BotToken != "" {
		return checkSlackAPIResponse(resp)
	}
	return nil
}

// checkSlackAPIResponse reads the answer of the Web API, which reports failures as HTTP 200 with ok=false
func checkSlackAPIResponse(resp *http.Response) error {
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
	return nil
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

	payload, err := s.buildPayload(ctx, cfg, message, monitor, heartbeat)
	if err != nil {
		return err
	}

	// Convert payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Slack payload: %w", err)
	}

	s.logger.Debugf("Slack payload: %s", string(jsonPayload))

	req, err := s.newRequest(ctx, cfg, jsonPayload)
	if err != nil {
		return err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Slack message: %w", withoutRequestURL(err))
	}
	defer resp.Body.Close()

	if err := checkSlackResponse(resp, cfg); err != nil {
		return err
	}

	s.logger.Infof("Slack message sent successfully")
	return nil
}
