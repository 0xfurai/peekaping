package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/version"

	"go.uber.org/zap"
)

type PushoverConfig struct {
	UserKey  string `json:"pushover_user_key" validate:"required" secret:"true"`
	AppToken string `json:"pushover_app_token" validate:"required" secret:"true"`
	Device   string `json:"pushover_device"`
	Title    string `json:"pushover_title"`
	Priority int    `json:"pushover_priority" validate:"min=-2,max=2"`
	Sounds   string `json:"pushover_sounds"`
	SoundsUp string `json:"pushover_sounds_up"`
	TTL      int    `json:"pushover_ttl" validate:"min=0"`
}

type PushoverSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewPushoverSender creates a new PushoverSender
func NewPushoverSender(logger *zap.SugaredLogger, client *http.Client) *PushoverSender {
	return &PushoverSender{
		logger: logger,
		client: client,
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

// pushoverPayload is the request payload: the message with the monitor's name and the time of the
// heartbeat if there are any, and the options of the channel
func pushoverPayload(cfg *PushoverConfig, message string, m *monitor.Model, hb *heartbeat.Model) map[string]interface{} {
	finalMessage := message
	if m != nil && m.Name != "" {
		finalMessage = fmt.Sprintf("<b>%s</b>\n%s", m.Name, message)
	}

	// Add timestamp if heartbeat is available
	if hb != nil {
		finalMessage = fmt.Sprintf("%s\n\n<b>Time:</b> %s", finalMessage, hb.Time.Format("2006-01-02 15:04:05"))
	}

	// Prepare the request payload
	payload := map[string]interface{}{
		"message": finalMessage,
		"user":    cfg.UserKey,
		"token":   cfg.AppToken,
		"html":    1,
		"retry":   "30",
		"expire":  "3600",
		"title":   pushoverTitle(cfg),
		// Set priority (default to 0 if not specified)
		"priority": cfg.Priority,
	}

	// Set optional fields
	if cfg.Device != "" {
		payload["device"] = cfg.Device
	}

	if sound := pushoverSound(cfg, hb); sound != "" {
		payload["sound"] = sound
	}

	// Set TTL if specified
	if cfg.TTL > 0 {
		payload["ttl"] = cfg.TTL
	}
	return payload
}

// pushoverTitle is the configured title, or a default
func pushoverTitle(cfg *PushoverConfig) string {
	if cfg.Title != "" {
		return cfg.Title
	}
	return "Peekaping Notification"
}

// pushoverSound is the sound for an Up if there is one, and otherwise the sound of the channel
func pushoverSound(cfg *PushoverConfig, hb *heartbeat.Model) string {
	if hb != nil && hb.Status == shared.MonitorStatusUp && cfg.SoundsUp != "" {
		return cfg.SoundsUp
	}
	return cfg.Sounds
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

	p.logger.Infof("Sending Pushover notification")

	payload := pushoverPayload(cfg, message, monitor, heartbeat)

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
