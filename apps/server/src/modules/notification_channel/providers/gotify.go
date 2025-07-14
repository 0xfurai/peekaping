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

	// Set default title if not provided
	title := "Peekaping"
	if cfg.Title != "" {
		title = cfg.Title
		// Apply template variable replacement to title
		title = strings.ReplaceAll(title, "{{ msg }}", message)
		if monitor != nil {
			title = strings.ReplaceAll(title, "{{ name }}", monitor.Name)
		}
		if heartbeat != nil {
			status := humanReadableStatus(int(heartbeat.Status))
			title = strings.ReplaceAll(title, "{{ status }}", status)
		}
	}

	// Prepare message content
	finalMessage := message
	if cfg.CustomMessage != "" {
		// Use simple string replacement for template variables
		finalMessage = strings.ReplaceAll(cfg.CustomMessage, "{{ msg }}", message)
		if monitor != nil {
			finalMessage = strings.ReplaceAll(finalMessage, "{{ name }}", monitor.Name)
		}
		if heartbeat != nil {
			status := humanReadableStatus(int(heartbeat.Status))
			finalMessage = strings.ReplaceAll(finalMessage, "{{ status }}", status)
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
