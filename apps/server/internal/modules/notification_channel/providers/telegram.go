package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"strings"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

type TelegramConfig struct {
	BotToken          string `json:"bot_token" validate:"required" secret:"true"`
	ChatID            string `json:"chat_id" validate:"required"`
	MessageThreadID   string `json:"message_thread_id"`
	ServerUrl         string `json:"server_url" validate:"omitempty,url" endpoint:"true" endpointDefault:"https://api.telegram.org" urlCredentials:"true" target:"url"`
	UseTemplate       bool   `json:"use_template"`
	TemplateParseMode string `json:"template_parse_mode" validate:"omitempty,oneof=plain MarkdownV2 HTML Markdown"`
	Template          string `json:"template"`
	SendSilently      bool   `json:"send_silently"`
	ProtectContent    bool   `json:"protect_content"`
}

type TelegramSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewTelegramSender creates an TelegramSender
func NewTelegramSender(logger *zap.SugaredLogger, client *http.Client) *TelegramSender {
	return &TelegramSender{logger: logger, client: client}
}

func (s *TelegramSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[TelegramConfig](configJSON)
}

func (s *TelegramSender) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*TelegramConfig))
}

// telegramParams are the parameters of the sendMessage call: the message, or the template
// rendered, with its parse mode
func telegramParams(cfg *TelegramConfig, message string, monitor *monitor.Model, heartbeat *heartbeat.Model) (map[string]any, error) {
	params := map[string]any{
		"chat_id":              cfg.ChatID,
		"text":                 message,
		"disable_notification": cfg.SendSilently,
		"protect_content":      cfg.ProtectContent,
	}
	if cfg.MessageThreadID != "" {
		params["message_thread_id"] = cfg.MessageThreadID
	}

	if cfg.UseTemplate {
		if err := addTelegramTemplate(params, cfg, PrepareTemplateBindings(monitor, heartbeat, message)); err != nil {
			return nil, err
		}
	}
	return params, nil
}

// addTelegramTemplate renders the template as the text of the message, and sets the parse mode
func addTelegramTemplate(params map[string]any, cfg *TelegramConfig, bindings map[string]any) error {
	if cfg.Template != "" {
		rendered, err := liquid.NewEngine().ParseAndRenderString(cfg.Template, bindings)
		if err != nil {
			return fmt.Errorf("failed to render template: %w", err)
		}
		params["text"] = rendered
	}

	if cfg.TemplateParseMode != "plain" && cfg.TemplateParseMode != "" {
		params["parse_mode"] = cfg.TemplateParseMode
	}
	return nil
}

// telegramStatusError is the error for a response that is not a success, with Telegram's description
func telegramStatusError(resp *http.Response, botToken string) error {
	// A bounded read: the error carries Telegram's description, however long the response is
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	// A self-hosted Bot API server may echo the request path, and the path holds the token
	text := strings.ReplaceAll(strings.TrimSpace(string(body)), botToken, "[redacted]")
	return fmt.Errorf("telegram API returned status: %s, body: %s", resp.Status, text)
}

func (s *TelegramSender) Send(
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
	cfg := cfgAny.(*TelegramConfig)

	s.logger.Infof("Sending telegram message: %s", message)

	url := cfg.ServerUrl
	if url == "" {
		url = "https://api.telegram.org"
	}

	params, err := telegramParams(cfg, message, monitor, heartbeat)
	if err != nil {
		return err
	}

	apiUrl := fmt.Sprintf("%s/bot%s/sendMessage", url, cfg.BotToken)

	// Prepare request
	req, err := http.NewRequestWithContext(ctx, "GET", apiUrl, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add params as query
	q := req.URL.Query()
	for k, v := range params {
		q.Add(k, fmt.Sprintf("%v", v))
	}
	req.URL.RawQuery = q.Encode()

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send telegram message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return telegramStatusError(resp, cfg.BotToken)
	}

	s.logger.Infof("Telegram notification sent successfully")
	return nil
}
