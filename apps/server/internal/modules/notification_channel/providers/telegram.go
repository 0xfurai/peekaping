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
	ServerUrl         string `json:"server_url" validate:"omitempty,url" endpoint:"true" endpointDefault:"https://api.telegram.org"`
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
		engine := liquid.NewEngine()

		bindings := PrepareTemplateBindings(monitor, heartbeat, message)

		if cfg.Template != "" {
			if rendered, err := engine.ParseAndRenderString(cfg.Template, bindings); err == nil {
				params["text"] = rendered
			} else {
				return fmt.Errorf("failed to render template: %w", err)
			}
		}

		if cfg.TemplateParseMode != "plain" && cfg.TemplateParseMode != "" {
			params["parse_mode"] = cfg.TemplateParseMode
		}
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
		// A bounded read: the error carries Telegram's description, however long the response is
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		// A self-hosted Bot API server may echo the request path, and the path holds the token
		text := strings.ReplaceAll(strings.TrimSpace(string(body)), cfg.BotToken, "[redacted]")
		return fmt.Errorf("telegram API returned status: %s, body: %s", resp.Status, text)
	}

	s.logger.Infof("Telegram notification sent successfully")
	return nil
}
