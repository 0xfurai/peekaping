package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"strings"

	"go.uber.org/zap"
)

type TwilioConfig struct {
	AccountSID  string `json:"twilio_account_sid" validate:"required"`
	ApiKey      string `json:"twilio_api_key"`
	AuthToken   string `json:"twilio_auth_token" validate:"required"`
	FromNumber  string `json:"twilio_from_number" validate:"required,e164"`
	ToNumber    string `json:"twilio_to_number" validate:"required,e164"`
}

type TwilioSender struct {
	logger *zap.SugaredLogger
}

// NewTwilioSender creates a TwilioSender
func NewTwilioSender(logger *zap.SugaredLogger) *TwilioSender {
	return &TwilioSender{logger: logger}
}

func (s *TwilioSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[TwilioConfig](configJSON)
}

func (s *TwilioSender) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*TwilioConfig))
}

func (s *TwilioSender) Send(
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
	cfg := cfgAny.(*TwilioConfig)

	s.logger.Infof("Sending Twilio SMS to: %s", cfg.ToNumber)

	// Prepare the authentication key
	// If API key is provided, use it; otherwise use Account SID
	apiKey := cfg.ApiKey
	if apiKey == "" {
		apiKey = cfg.AccountSID
	}

	// Prepare the request
	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", cfg.AccountSID)

	data := url.Values{}
	data.Set("To", cfg.ToNumber)
	data.Set("From", cfg.FromNumber)
	data.Set("Body", message)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.SetBasicAuth(apiKey, cfg.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Twilio SMS: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var respBody map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&respBody); err == nil {
			if msg, ok := respBody["message"].(string); ok {
				return fmt.Errorf("twilio API error: %s (status: %d)", msg, resp.StatusCode)
			}
		}
		return fmt.Errorf("twilio API returned status: %d", resp.StatusCode)
	}

	s.logger.Infof("Twilio SMS sent successfully to: %s", cfg.ToNumber)
	return nil
}
