package providers

import (
	"context"
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

type NTFYConfig struct {
	ServerUrl          string `json:"server_url" validate:"required" endpoint:"true" urlCredentials:"true" target:"url"`
	Topic              string `json:"topic" validate:"required"`
	AuthenticationType string `json:"authentication_type" validate:"required,oneof=none basic token"`
	Username           string `json:"username"`
	Password           string `json:"password" secret:"true"`
	Token              string `json:"token" secret:"true"`
	Priority           int    `json:"priority" validate:"min=1,max=5"`
	Tags               string `json:"tags"`
	Title              string `json:"title"`
	CustomMessage      string `json:"custom_message"`
}

type NTFYSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewNTFYSender creates an NTFYSender
func NewNTFYSender(logger *zap.SugaredLogger, client *http.Client) *NTFYSender {
	return &NTFYSender{
		logger: logger,
		client: client,
	}
}

func (e *NTFYSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[NTFYConfig](configJSON)
}

func (e *NTFYSender) Validate(configJSON string) error {
	cfg, err := e.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*NTFYConfig))
}

// renderOr renders text as a template. It gives fallback for text that is left out, and for text
// that can't be rendered, which what names in the warning.
func (e *NTFYSender) renderOr(engine *liquid.Engine, bindings map[string]any, text, what, fallback string) string {
	if text == "" {
		return fallback
	}
	rendered, err := engine.ParseAndRenderString(text, bindings)
	if err != nil {
		e.logger.Warnf("Failed to render %s template: %v", what, err)
		return fallback
	}
	return rendered
}

// renderTags renders the tags as a template, and splits them at the commas. It gives none for tags
// that are left out, and for tags that can't be rendered.
func (e *NTFYSender) renderTags(engine *liquid.Engine, bindings map[string]any, text string) []string {
	if text == "" {
		return nil
	}
	rendered, err := engine.ParseAndRenderString(text, bindings)
	if err != nil {
		e.logger.Warnf("Failed to render tags template: %v", err)
		return nil
	}
	tags := strings.Split(rendered, ",")
	// Trim whitespace from each tag
	for i, tag := range tags {
		tags[i] = strings.TrimSpace(tag)
	}
	return tags
}

// ntfyPriority is the configured priority, and 3 if there is none
func ntfyPriority(cfg *NTFYConfig) int {
	if cfg.Priority == 0 {
		return 3 // Default priority
	}
	return cfg.Priority
}

// ntfySetHeaders sets the headers of the request: the title, priority and tags that there are
func ntfySetHeaders(req *http.Request, title string, priority int, tags []string) {
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("User-Agent", "Peekaping-NTFY/"+version.Version)

	// Set title header if provided
	if title != "" {
		req.Header.Set("X-Title", title)
	}

	// Set priority header if provided
	if priority > 0 {
		req.Header.Set("X-Priority", fmt.Sprintf("%d", priority))
	}

	// Set tags header if provided
	if len(tags) > 0 {
		req.Header.Set("X-Tags", strings.Join(tags, ","))
	}
}

// ntfyAuthenticate sets the request's credentials for the configured type of authentication
func ntfyAuthenticate(req *http.Request, cfg *NTFYConfig) error {
	switch cfg.AuthenticationType {
	case "basic":
		if cfg.Username == "" || cfg.Password == "" {
			return fmt.Errorf("username and password required for basic authentication")
		}
		req.SetBasicAuth(cfg.Username, cfg.Password)
	case "token":
		if cfg.Token == "" {
			return fmt.Errorf("token required for token authentication")
		}
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	case "none":
		// No authentication required
	default:
		return fmt.Errorf("unsupported authentication type: %s", cfg.AuthenticationType)
	}
	return nil
}

func (e *NTFYSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	m *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := e.Unmarshal(configJSON)
	if err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	cfg := cfgAny.(*NTFYConfig)

	engine := liquid.NewEngine()
	bindings := PrepareTemplateBindings(m, heartbeat, message)

	// Prepare message content
	finalMessage := e.renderOr(engine, bindings, cfg.CustomMessage, "custom message", message)

	// Prepare title
	finalTitle := e.renderOr(engine, bindings, cfg.Title, "title", "Peekaping Notification")

	// Prepare tags
	tags := e.renderTags(engine, bindings, cfg.Tags)

	// Set default priority if not specified
	priority := ntfyPriority(cfg)

	// Prepare request URL
	url := fmt.Sprintf("%s/%s", strings.TrimSuffix(cfg.ServerUrl, "/"), cfg.Topic)

	// Create request with plain text message
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(finalMessage))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	ntfySetHeaders(req, finalTitle, priority, tags)

	// Handle authentication
	if err := ntfyAuthenticate(req, cfg); err != nil {
		return err
	}

	// Send request
	e.logger.Infof("Sending NTFY notification")
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send NTFY request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("NTFY request failed with status %d: %s", resp.StatusCode, string(body))
	}

	e.logger.Infof("NTFY notification sent successfully")
	return nil
}
