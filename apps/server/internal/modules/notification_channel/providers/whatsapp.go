package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/version"
	"strings"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

type WhatsAppConfig struct {
	ServerURL     string `json:"server_url" validate:"required,url" endpoint:"true" urlCredentials:"true" target:"url"`
	APIKey        string `json:"api_key" secret:"true"`
	PhoneNumber   string `json:"phone_number" validate:"required"`
	Session       string `json:"session" validate:"required"`
	UseTemplate   bool   `json:"use_template"`
	Template      string `json:"template"`
	CustomMessage string `json:"custom_message"`
}

type WhatsAppSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewWhatsAppSender creates a WhatsAppSender
func NewWhatsAppSender(logger *zap.SugaredLogger, client *http.Client) *WhatsAppSender {
	return &WhatsAppSender{
		logger: logger,
		client: client,
	}
}

func (w *WhatsAppSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[WhatsAppConfig](configJSON)
}

func (w *WhatsAppSender) Validate(configJSON string) error {
	cfg, err := w.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*WhatsAppConfig))
}

func (w *WhatsAppSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	monitor *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := w.Unmarshal(configJSON)
	if err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	cfg := cfgAny.(*WhatsAppConfig)

	engine := liquid.NewEngine()
	bindings := PrepareTemplateBindings(monitor, heartbeat, message)

	// Prepare message content
	finalMessage := message
	if cfg.CustomMessage != "" {
		if rendered, err := engine.ParseAndRenderString(cfg.CustomMessage, bindings); err == nil {
			finalMessage = rendered
		} else {
			w.logger.Warnf("Failed to render custom message template: %v", err)
		}
	}

	// Use template if enabled
	if cfg.UseTemplate && cfg.Template != "" {
		if rendered, err := engine.ParseAndRenderString(cfg.Template, bindings); err == nil {
			finalMessage = rendered
		} else {
			w.logger.Warnf("Failed to render template: %v", err)
		}
	}

	// Send message to phone number
	// The caller logs the error once it has taken the request URL out of it
	if err := w.sendToPhoneNumber(ctx, cfg, cfg.PhoneNumber, finalMessage); err != nil {
		return err
	}
	w.logger.Infof("WhatsApp message sent successfully to %s", cfg.PhoneNumber)

	return nil
}

// whatsAppPhoneCleaner removes the spaces, dashes, brackets and plus sign of a phone number
var whatsAppPhoneCleaner = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", "+", "")

func (w *WhatsAppSender) sendToPhoneNumber(
	ctx context.Context,
	cfg *WhatsAppConfig,
	phoneNumber string,
	message string,
) error {
	// Clean phone number (remove spaces, dashes, etc.)
	cleanPhone := whatsAppPhoneCleaner.Replace(phoneNumber)

	// Prepare the request payload
	payload := map[string]interface{}{
		"session": cfg.Session,
		"chatId":  cleanPhone,
		"text":    message,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create the request URL
	url := fmt.Sprintf("%s/api/sendText", strings.TrimSuffix(cfg.ServerURL, "/"))

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonPayload)))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping-WhatsApp/"+version.Version)
	if cfg.APIKey != "" {
		req.Header.Set("x-api-key", cfg.APIKey)
	}

	// Send the request
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		w.logger.Errorf("Failed to read response body: %v", err)
	}

	return w.checkResponse(resp.StatusCode, bodyBytes)
}

// checkResponse is the error for a response that is not a success, or whose body says that the
// message failed
func (w *WhatsAppSender) checkResponse(statusCode int, bodyBytes []byte) error {
	// Check response status
	if statusCode < 200 || statusCode >= 300 {
		return whatsAppStatusError(statusCode, bodyBytes)
	}

	// Parse response to check for success
	var response map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		w.logger.Warnf("Failed to parse response body: %v", err)
		// Don't return error here as the message might have been sent successfully
	}

	// Check if the response indicates success
	if success, ok := response["success"].(bool); ok && !success {
		if errorMsg, ok := response["message"].(string); ok {
			return fmt.Errorf("WAHA API reported failure: %s", errorMsg)
		}
		return fmt.Errorf("WAHA API reported failure")
	}
	return nil
}

// whatsAppStatusError is the error for a response that is not a success: the reason the server
// gives when its body has one
func whatsAppStatusError(statusCode int, bodyBytes []byte) error {
	// Try to read error response body
	var errorResponse map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &errorResponse); err == nil {
		if errorMsg, ok := errorResponse["message"].(string); ok {
			return fmt.Errorf("WAHA API error: %s (status: %d)", errorMsg, statusCode)
		}
	}
	return fmt.Errorf("WAHA API returned status: %d", statusCode)
}
