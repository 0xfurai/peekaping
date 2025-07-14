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

// GotifyConfig holds the configuration for Gotify notifications
type GotifyConfig struct {
	ServerURL        string `json:"server_url" validate:"required,url"`
	ApplicationToken string `json:"application_token" validate:"required"`
	Priority         *int   `json:"priority" validate:"omitempty,min=0,max=10"`
	Title            string `json:"title"`
	CustomMessage    string `json:"custom_message"`
}

// GotifySender handles sending notifications to Gotify
type GotifySender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewGotifySender creates a new GotifySender
func NewGotifySender(logger *zap.SugaredLogger) *GotifySender {
	return &GotifySender{
		logger: logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (g *GotifySender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[GotifyConfig](configJSON)
}

func (g *GotifySender) Validate(configJSON string) error {
	cfg, err := g.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*GotifyConfig))
}

// Send sends a notification to Gotify
func (g *GotifySender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	monitor *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := g.Unmarshal(configJSON)
	if err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	cfg := cfgAny.(*GotifyConfig)

	// Clean up server URL by removing trailing slash
	serverURL := strings.TrimSuffix(cfg.ServerURL, "/")

	// Prepare template bindings
	bindings := PrepareTemplateBindings(monitor, heartbeat, message)
	engine := liquid.NewEngine()

	// Set default title if not provided
	title := "Peekaping"
	if cfg.Title != "" {
		// Use liquid templating for title
		if rendered, err := engine.ParseAndRenderString(cfg.Title, bindings); err == nil {
			title = rendered
		} else {
			g.logger.Warnf("Failed to render title template: %v", err)
			title = cfg.Title // Fallback to original title
		}
	}

	// Prepare message content
	finalMessage := message
	if cfg.CustomMessage != "" {
		// Use liquid templating for custom message
		if rendered, err := engine.ParseAndRenderString(cfg.CustomMessage, bindings); err == nil {
			finalMessage = rendered
		} else {
			g.logger.Warnf("Failed to render custom message template: %v", err)
			finalMessage = cfg.CustomMessage // Fallback to original custom message
		}
	}

	// Set default priority if not specified
	priority := 8 // Default priority
	if cfg.Priority != nil {
		priority = *cfg.Priority
	}

	// Prepare request payload
	payload := map[string]interface{}{
		"title":    title,
		"message":  finalMessage,
		"priority": priority,
	}

	// Convert payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Build request URL
	requestURL := fmt.Sprintf("%s/message?token=%s", serverURL, cfg.ApplicationToken)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", requestURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping-Gotify/"+version.Version)

	// Send request
	g.logger.Infof("Sending Gotify notification to %s", serverURL)
	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Gotify request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Gotify request failed with status %d", resp.StatusCode)
	}

	g.logger.Infof("Gotify notification sent successfully to %s", serverURL)
	return nil
}
