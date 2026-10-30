package providers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/version"
	"strconv"
	"time"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

// Header names used when webhook signing is enabled.
const (
	webhookSignatureHeader = "X-Webhook-Signature"
	webhookTimestampHeader = "X-Webhook-Timestamp"
)

// signWebhookPayload returns "sha256=" and the lowercase hex HMAC-SHA256 of
// "{timestamp}.{payload}" keyed with secret. Receivers rebuild the same string
// from the X-Webhook-Timestamp header and the payload to verify it.
func signWebhookPayload(timestamp string, payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

type WebhookConfig struct {
	WebhookURL               string `json:"webhook_url" validate:"required,url" endpoint:"true" urlCredentials:"true" target:"url"`
	WebhookContentType       string `json:"webhook_content_type" validate:"required,oneof=json form-data custom"`
	WebhookCustomBody        string `json:"webhook_custom_body"`
	WebhookAdditionalHeaders string `json:"webhook_additional_headers" secret:"true"`
	WebhookSigningSecret     string `json:"webhook_signing_secret" secret:"true"`
}

type WebhookSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewWebhookSender creates a WebhookSender
func NewWebhookSender(logger *zap.SugaredLogger, client *http.Client) *WebhookSender {
	return &WebhookSender{logger: logger, client: client}
}

// invalidAdditionalHeaders is the error for additional headers that are not a JSON object. It
// says where the JSON is wrong but not what it holds, since the headers usually carry a token.
func invalidAdditionalHeaders(err error) error {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return fmt.Errorf("additional Headers is not a valid JSON (it has a syntax error at byte %d)", syntaxErr.Offset)
	}
	return errors.New("additional Headers is not a valid JSON object")
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

// webhookBody is the body of a webhook request
type webhookBody struct {
	content []byte
	// signed is what the signature covers: the body, except for form-data
	signed      []byte
	contentType string
}

// jsonBody is the data as JSON, without template support
func (w *WebhookSender) jsonBody(data map[string]any) (webhookBody, error) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return webhookBody{}, fmt.Errorf("failed to marshal JSON body: %w", err)
	}
	return webhookBody{content: jsonBytes, signed: jsonBytes, contentType: "application/json"}, nil
}

// formDataBody is the data as JSON in the data field of a form
func (w *WebhookSender) formDataBody(data map[string]any) (webhookBody, error) {
	// Create form-data with data field containing JSON string
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return webhookBody{}, fmt.Errorf("failed to marshal form data: %w", err)
	}

	// Write the data field with JSON string value
	err = writer.WriteField("data", string(jsonBytes))
	if err != nil {
		return webhookBody{}, fmt.Errorf("failed to write form data field: %w", err)
	}

	writer.Close()

	// Debug logging
	w.logger.Debugf("Form-data content-type: %s", writer.FormDataContentType())
	w.logger.Debugf("Form-data body length: %d bytes", buf.Len())

	// Sign the data field: receivers that parse multipart (PHP's $_POST) never
	// see the raw body with its random boundary
	return webhookBody{content: buf.Bytes(), signed: jsonBytes, contentType: writer.FormDataContentType()}, nil
}

// customBody is the custom body, rendered as a template
func customBody(cfg *WebhookConfig, message string, monitor *monitor.Model, heartbeat *heartbeat.Model) (webhookBody, error) {
	if cfg.WebhookCustomBody == "" {
		return webhookBody{}, fmt.Errorf("custom body is required when content type is custom")
	}

	// Render template for custom body
	bindings := PrepareTemplateBindings(monitor, heartbeat, message)
	engine := liquid.NewEngine()
	rendered, err := engine.ParseAndRenderString(cfg.WebhookCustomBody, bindings)
	if err != nil {
		return webhookBody{}, fmt.Errorf("failed to render custom body template: %w", err)
	}

	bodyBytes := []byte(rendered)
	return webhookBody{content: bodyBytes, signed: bodyBytes, contentType: "text/plain"}, nil
}

// buildBody is the body of the content type the channel is set to
func (w *WebhookSender) buildBody(cfg *WebhookConfig, message string, monitor *monitor.Model, heartbeat *heartbeat.Model) (webhookBody, error) {
	// Prepare simple data structure matching JS implementation
	data := map[string]any{
		"heartbeat": heartbeat,
		"monitor":   monitor,
		"msg":       message,
	}

	switch cfg.WebhookContentType {
	case "json":
		return w.jsonBody(data)
	case "form-data":
		return w.formDataBody(data)
	case "custom":
		return customBody(cfg, message, monitor, heartbeat)
	default:
		return webhookBody{}, fmt.Errorf("unsupported content type: %s", cfg.WebhookContentType)
	}
}

// addAdditionalHeaders sets the headers of the config, a JSON object, on the request
func addAdditionalHeaders(req *http.Request, additional string) error {
	if additional == "" {
		return nil
	}
	// The headers usually carry a token: they stay out of the log and out of the error
	var additionalHeaders map[string]any
	if err := json.Unmarshal([]byte(additional), &additionalHeaders); err != nil {
		return invalidAdditionalHeaders(err)
	}

	for key, value := range additionalHeaders {
		req.Header.Set(key, fmt.Sprintf("%v", value))
	}
	return nil
}

// newRequest makes the request that posts the body, with the content type, the additional headers
// and the signature if there is a secret
func (w *WebhookSender) newRequest(ctx context.Context, cfg *WebhookConfig, body webhookBody) (*http.Request, error) {
	// Create HTTP request (always POST)
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.WebhookURL, bytes.NewReader(body.content))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", body.contentType)

	// Parse and set additional headers
	if err := addAdditionalHeaders(req, cfg.WebhookAdditionalHeaders); err != nil {
		return nil, err
	}

	// Sign after the additional headers so they can't replace the signature.
	// The timestamp is signed too, so receivers can reject old requests.
	if cfg.WebhookSigningSecret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		req.Header.Set(webhookSignatureHeader, signWebhookPayload(timestamp, body.signed, cfg.WebhookSigningSecret))
		req.Header.Set(webhookTimestampHeader, timestamp)
	}

	// Set default user agent
	req.Header.Set("User-Agent", "Peekaping-Webhook/"+version.Version)
	return req, nil
}

// clientFor is the client to send with. A signed request is not sent through redirects.
func (w *WebhookSender) clientFor(cfg *WebhookConfig) *http.Client {
	if cfg.WebhookSigningSecret == "" {
		return w.client
	}
	// A redirect would hand the signed request to another endpoint, which could
	// replay it here while the timestamp is fresh, so a 3xx fails the send instead
	noRedirects := *w.client
	noRedirects.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &noRedirects
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

	w.logger.Infof("Sending webhook notification")

	// Prepare request body and headers based on content type
	body, err := w.buildBody(cfg, message, monitor, heartbeat)
	if err != nil {
		return err
	}

	req, err := w.newRequest(ctx, cfg, body)
	if err != nil {
		return err
	}

	resp, err := w.clientFor(cfg).Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	w.logger.Infof("Webhook notification sent successfully")
	return nil
}
