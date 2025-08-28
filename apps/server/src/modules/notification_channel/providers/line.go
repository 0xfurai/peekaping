package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"time"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

type LineConfig struct {
	ChannelAccessToken string `json:"channel_access_token" validate:"required"`
	UserID             string `json:"user_id" validate:"required"`
	Template           string `json:"template"`
}

type LineSender struct {
	logger *zap.SugaredLogger
}

func NewLineSender(logger *zap.SugaredLogger) *LineSender {
	return &LineSender{logger: logger}
}

func (s *LineSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[LineConfig](configJSON)
}

func (s *LineSender) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*LineConfig))
}

func (s *LineSender) Send(
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
	cfg := cfgAny.(*LineConfig)

	s.logger.Infof("Sending LINE message: %s", message)

	url := "https://api.line.me/v2/bot/message/push"

	// Prepare message text
	messageText := message
	if cfg.Template != "" {
		engine := liquid.NewEngine()
		bindings := PrepareTemplateBindings(monitor, heartbeat, message)

		if s.logger != nil {
			jsonDebug, _ := json.MarshalIndent(bindings, "", "  ")
			s.logger.Debugf("Template bindings: %s", string(jsonDebug))
		}

		if rendered, err := engine.ParseAndRenderString(cfg.Template, bindings); err == nil {
			messageText = rendered
		} else {
			return fmt.Errorf("failed to render template: %w", err)
		}
	} else if heartbeat != nil {
		// Default message format when template is not used
		if heartbeat.Status == 0 { // DOWN
			messageText = fmt.Sprintf("Peekaping Alert: [🔴 Down]\nName: %s\n%s\nTime: %s",
				monitor.Name,
				message,
				heartbeat.Time.Format(time.RFC3339))
		} else if heartbeat.Status == 1 { // UP
			messageText = fmt.Sprintf("Peekaping Alert: [✅ Up]\nName: %s\n%s\nTime: %s",
				monitor.Name,
				message,
				heartbeat.Time.Format(time.RFC3339))
		}
	} else {
		// Test message
		messageText = "Test Successful!"
	}

	// Prepare LINE message payload
	payload := map[string]interface{}{
		"to": cfg.UserID,
		"messages": []map[string]interface{}{
			{
				"type": "text",
				"text": messageText,
			},
		},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal LINE payload: %w", err)
	}

	// Prepare request
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.ChannelAccessToken)

	s.logger.Debugf("Sending LINE message to user: %s", cfg.UserID)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send LINE message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errorBody map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&errorBody); err == nil {
			if msg, ok := errorBody["message"].(string); ok {
				return fmt.Errorf("LINE API error: %s (status: %d)", msg, resp.StatusCode)
			}
		}
		return fmt.Errorf("LINE API returned status: %s", resp.Status)
	}

	s.logger.Infof("LINE message sent successfully")
	return nil
}