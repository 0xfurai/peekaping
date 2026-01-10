package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/monitor"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

type TelegramConfig struct {
	BotToken          string `json:"bot_token" validate:"required"`
	ChatID            string `json:"chat_id" validate:"required"`
	MessageThreadID   string `json:"message_thread_id"`
	ServerUrl         string `json:"server_url" validate:"omitempty,url"`
	UseTemplate       bool   `json:"use_template"`
	TemplateParseMode string `json:"template_parse_mode" validate:"omitempty,oneof=plain MarkdownV2 HTML Markdown"`
	Template          string `json:"template"`
	SendSilently      bool   `json:"send_silently"`
	ProtectContent    bool   `json:"protect_content"`
}

type TelegramSender struct {
	logger *zap.SugaredLogger
}

// NewTelegramSender creates an TelegramSender
func NewTelegramSender(logger *zap.SugaredLogger) *TelegramSender {
	return &TelegramSender{logger: logger}
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

		// Optional: Add debug output for development
		if s.logger != nil {
			jsonDebug, _ := json.MarshalIndent(bindings, "", "  ")
			s.logger.Debugf("Template bindings: %s", string(jsonDebug))
		}

		if cfg.Template != "" {
			if rendered, err := engine.ParseAndRenderString(cfg.Template, bindings); err == nil {
				params["text"] = rendered
			} else {
				return fmt.Errorf("failed to render template: %w", err)
			}
		}

		fmt.Println("parse_mode", cfg.TemplateParseMode)
		if cfg.TemplateParseMode != "plain" && cfg.TemplateParseMode != "" {
			params["parse_mode"] = cfg.TemplateParseMode
		}
	}

	apiUrl := fmt.Sprintf("%s/bot%s/sendMessage", url, cfg.BotToken)

	// Prepare request
	client := &http.Client{}
	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add params as query
	q := req.URL.Query()
	for k, v := range params {
		q.Add(k, fmt.Sprintf("%v", v))
	}
	req.URL.RawQuery = q.Encode()

	s.logger.Debugf("Sending telegram message: %s", req.URL.String())

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send telegram message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram API returned status: %s, body: %s", resp.Status, resp.Body)
	}

	return nil
}
