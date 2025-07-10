package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"peekaping/src/version"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

type WebhookConfig struct {
	WebhookURL               string `json:"webhook_url" validate:"required,url"`
	WebhookContentType       string `json:"webhook_content_type" validate:"required,oneof=json form-data custom"`
	WebhookCustomBody        string `json:"webhook_custom_body"`
	WebhookAdditionalHeaders string `json:"webhook_additional_headers"`
}

type WebhookSender struct {
	logger *zap.SugaredLogger
}

// NewWebhookSender creates a WebhookSender
func NewWebhookSender(logger *zap.SugaredLogger) *WebhookSender {
	return &WebhookSender{logger: logger}
}

func (w *WebhookSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[WebhookConfig](configJSON)
}

func (w *WebhookSender) Validate(configJSON string) error {
	cfg, err := w.Unmarshal(configJSON)
	if err != nil {
		return err
	}

	webhookCfg := cfg.(*WebhookConfig)

	// Validate custom body is provided when content type is custom
	if webhookCfg.WebhookContentType == "custom" && webhookCfg.WebhookCustomBody == "" {
		return fmt.Errorf("webhook_custom_body is required when webhook_content_type is 'custom'")
	}

	return GenericValidator(webhookCfg)
}

func (w *WebhookSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	monitor *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := w.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	cfg := cfgAny.(*WebhookConfig)

	w.logger.Infof("Sending webhook notification to: %s", cfg.WebhookURL)

	// Prepare simple data structure matching JS implementation
	data := map[string]any{
		"heartbeat": heartbeat,
		"monitor":   monitor,
		"msg":       message,
	}

	// Prepare request body and headers based on content type
	var body io.Reader
	headers := make(map[string]string)

	switch cfg.WebhookContentType {
	case "json":
		// Simple JSON payload without template support
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal JSON body: %w", err)
		}
		body = bytes.NewBuffer(jsonBytes)
		headers["Content-Type"] = "application/json"

	case "form-data":
		// Create form-data with data field containing JSON string
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		jsonBytes, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal form data: %w", err)
		}

		// Write the data field with JSON string value
		err = writer.WriteField("data", string(jsonBytes))
		if err != nil {
			return fmt.Errorf("failed to write form data field: %w", err)
		}

		writer.Close()

		body = &buf
		headers["Content-Type"] = writer.FormDataContentType()

		// Debug logging
		w.logger.Debugf("Form-data content-type: %s", headers["Content-Type"])
		w.logger.Debugf("Form-data body length: %d bytes", buf.Len())

	case "custom":
		if cfg.WebhookCustomBody == "" {
			return fmt.Errorf("custom body is required when content type is custom")
		}

		// Render template for custom body
		bindings := PrepareTemplateBindings(monitor, heartbeat, message)
		engine := liquid.NewEngine()
		rendered, err := engine.ParseAndRenderString(cfg.WebhookCustomBody, bindings)
		if err != nil {
			return fmt.Errorf("failed to render custom body template: %w", err)
		}

		body = bytes.NewBufferString(rendered)
		headers["Content-Type"] = "text/plain"

	default:
		return fmt.Errorf("unsupported content type: %s", cfg.WebhookContentType)
	}

	// Create HTTP request (always POST)
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.WebhookURL, body)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Parse and set additional headers
	if cfg.WebhookAdditionalHeaders != "" {
		w.logger.Debugf("Parsing additional headers: %q", cfg.WebhookAdditionalHeaders)
		var additionalHeaders map[string]any
		if err := json.Unmarshal([]byte(cfg.WebhookAdditionalHeaders), &additionalHeaders); err != nil {
			return fmt.Errorf("additional Headers is not a valid JSON: %q - %w", cfg.WebhookAdditionalHeaders, err)
		}

		for key, value := range additionalHeaders {
			req.Header.Set(key, fmt.Sprintf("%v", value))
		}
	}

	// Set default user agent
	req.Header.Set("User-Agent", "Peekaping-Webhook/"+version.Version)

	w.logger.Debugf("Sending webhook POST request to: %s", cfg.WebhookURL)

	// Send request with default HTTP client
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	w.logger.Infof("Webhook notification sent successfully to: %s", cfg.WebhookURL)
	return nil
}
