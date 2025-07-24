package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"peekaping/src/modules/shared"
	"peekaping/src/version"
	"time"

	"go.uber.org/zap"
)

type PushoverConfig struct {
	UserKey    string `json:"pushover_user_key" validate:"required"`
	AppToken   string `json:"pushover_app_token" validate:"required"`
	Device     string `json:"pushover_device"`
	Title      string `json:"pushover_title"`
	Priority   int    `json:"pushover_priority" validate:"min=-2,max=2"`
	Sounds     string `json:"pushover_sounds"`
	SoundsUp   string `json:"pushover_sounds_up"`
	TTL        int    `json:"pushover_ttl" validate:"min=0"`
}

type PushoverSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewPushoverSender creates a new PushoverSender
func NewPushoverSender(logger *zap.SugaredLogger) *PushoverSender {
	return &PushoverSender{
		logger: logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (p *PushoverSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[PushoverConfig](configJSON)
}

func (p *PushoverSender) Validate(configJSON string) error {
	cfg, err := p.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*PushoverConfig))
}

func (p *PushoverSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	monitor *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := p.Unmarshal(configJSON)
	if err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	cfg := cfgAny.(*PushoverConfig)

	p.logger.Infof("Sending Pushover notification to user: %s", cfg.UserKey)

	// Prepare the request payload
	payload := map[string]interface{}{
		"message":  message,
		"user":     cfg.UserKey,
		"token":    cfg.AppToken,
		"html":     1,
		"retry":    "30",
		"expire":   "3600",
	}

	// Set optional fields
	if cfg.Device != "" {
		payload["device"] = cfg.Device
	}

	if cfg.Title != "" {
		payload["title"] = cfg.Title
	} else {
		payload["title"] = "Peekaping Notification"
	}

	// Set priority (default to 0 if not specified)
	payload["priority"] = cfg.Priority

	// Set sound
	sound := cfg.Sounds
	if heartbeat != nil && heartbeat.Status == shared.MonitorStatusUp && cfg.SoundsUp != "" {
		sound = cfg.SoundsUp
	}
	if sound != "" {
		payload["sound"] = sound
	}

	// Set TTL if specified
	if cfg.TTL > 0 {
		payload["ttl"] = cfg.TTL
	}

	// Add timestamp if heartbeat is available
	if heartbeat != nil {
		payload["message"] = fmt.Sprintf("%s\n\n<b>Time:</b> %s", message, heartbeat.Time.Format("2006-01-02 15:04:05"))
	}

	// Convert payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.pushover.net/1/messages.json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping-Pushover/"+version.Version)

	// Send request
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Pushover request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Pushover API returned status code: %d", resp.StatusCode)
	}

	p.logger.Infof("Pushover notification sent successfully")
	return nil
}