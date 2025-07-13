package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"peekaping/src/version"
	"strings"
	"time"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

type SignalConfig struct {
	SignalURL        string `json:"signal_url" validate:"required,url"`
	SignalNumber     string `json:"signal_number" validate:"required"`
	SignalRecipients string `json:"signal_recipients" validate:"required"`
	CustomMessage    string `json:"custom_message"`
}

type SignalSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewSignalSender creates a SignalSender
func NewSignalSender(logger *zap.SugaredLogger) *SignalSender {
	return &SignalSender{
		logger: logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (s *SignalSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[SignalConfig](configJSON)
}

func (s *SignalSender) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*SignalConfig))
}

func (s *SignalSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	monitor *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := s.Unmarshal(configJSON)
	if err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	cfg := cfgAny.(*SignalConfig)

	s.logger.Infof("Sending Signal message to: %s", cfg.SignalURL)

	// Prepare message content
	finalMessage := message
	if cfg.CustomMessage != "" {
		engine := liquid.NewEngine()
		bindings := PrepareTemplateBindings(monitor, heartbeat, message)
		
		if rendered, err := engine.ParseAndRenderString(cfg.CustomMessage, bindings); err == nil {
			finalMessage = rendered
		} else {
			s.logger.Warnf("Failed to render custom message template: %v", err)
		}
	}

	// Parse recipients - remove spaces and split by comma
	recipients := strings.Split(strings.ReplaceAll(cfg.SignalRecipients, " ", ""), ",")

	// Prepare the request payload
	payload := map[string]any{
		"message":    finalMessage,
		"number":     cfg.SignalNumber,
		"recipients": recipients,
	}

	// Convert payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON payload: %w", err)
	}

	// Create the HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.SignalURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping-Signal/"+version.Version)

	// Send the request
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Signal message: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Signal API returned status code: %d", resp.StatusCode)
	}

	s.logger.Infof("Signal message sent successfully to %s", cfg.SignalURL)
	return nil
}