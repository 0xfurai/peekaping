package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"peekaping/src/version"
	"strings"
	"time"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

type NTFYConfig struct {
	ServerUrl          string `json:"server_url" validate:"required"`
	Topic              string `json:"topic" validate:"required"`
	AuthenticationType string `json:"authentication_type" validate:"required,oneof=none basic token"`
	Username           string `json:"username"`
	Password           string `json:"password"`
	Token              string `json:"token"`
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
func NewNTFYSender(logger *zap.SugaredLogger) *NTFYSender {
	return &NTFYSender{
		logger: logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
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
	finalMessage := message
	if cfg.CustomMessage != "" {
		if rendered, err := engine.ParseAndRenderString(cfg.CustomMessage, bindings); err == nil {
			finalMessage = rendered
		} else {
			e.logger.Warnf("Failed to render custom message template: %v", err)
		}
	}

	// Prepare title
	finalTitle := "Peekaping Notification"
	if cfg.Title != "" {
		if rendered, err := engine.ParseAndRenderString(cfg.Title, bindings); err == nil {
			finalTitle = rendered
		} else {
			e.logger.Warnf("Failed to render title template: %v", err)
		}
	}

	// Prepare tags
	var tags []string
	if cfg.Tags != "" {
		if rendered, err := engine.ParseAndRenderString(cfg.Tags, bindings); err == nil {
			tags = strings.Split(rendered, ",")
			// Trim whitespace from each tag
			for i, tag := range tags {
				tags[i] = strings.TrimSpace(tag)
			}
		} else {
			e.logger.Warnf("Failed to render tags template: %v", err)
		}
	}

	// Set default priority if not specified
	priority := cfg.Priority
	if priority == 0 {
		priority = 3 // Default priority
	}

	// Prepare request URL
	url := fmt.Sprintf("%s/%s", strings.TrimSuffix(cfg.ServerUrl, "/"), cfg.Topic)

	// Create request with plain text message
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(finalMessage))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("User-Agent", "Peekaping-NTFY/"+version.Version)

	// Set title header if provided
	if finalTitle != "" {
		req.Header.Set("X-Title", finalTitle)
	}

	// Set priority header if provided
	if priority > 0 {
		req.Header.Set("X-Priority", fmt.Sprintf("%d", priority))
	}

	// Set tags header if provided
	if len(tags) > 0 {
		req.Header.Set("X-Tags", strings.Join(tags, ","))
	}

	// Handle authentication
	switch cfg.AuthenticationType {
	case "basic":
		if cfg.Username != "" && cfg.Password != "" {
			req.SetBasicAuth(cfg.Username, cfg.Password)
		} else {
			return fmt.Errorf("username and password required for basic authentication")
		}
	case "token":
		if cfg.Token != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
		} else {
			return fmt.Errorf("token required for token authentication")
		}
	case "none":
		// No authentication required
	default:
		return fmt.Errorf("unsupported authentication type: %s", cfg.AuthenticationType)
	}

	// Send request
	e.logger.Infof("Sending NTFY notification to %s with topic %s", cfg.ServerUrl, cfg.Topic)
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

	e.logger.Infof("NTFY notification sent successfully to %s", cfg.ServerUrl)
	return nil
}
