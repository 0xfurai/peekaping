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

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

// PushbulletConfig holds the configuration for Pushbullet notifications
type PushbulletConfig struct {
	AccessToken    string `json:"pushbullet_access_token" validate:"required" secret:"true"`
	DeviceID       string `json:"pushbullet_device_id"`       // Optional: specific device
	ChannelTag     string `json:"pushbullet_channel_tag"`     // Optional: channel tag
	CustomTemplate string `json:"pushbullet_custom_template"` // Optional: custom message template
}

// PushbulletSender handles sending notifications via Pushbullet
type PushbulletSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewPushbulletSender creates a new PushbulletSender instance
func NewPushbulletSender(logger *zap.SugaredLogger, client *http.Client) *PushbulletSender {
	return &PushbulletSender{logger: logger, client: client}
}

// Unmarshal parses the JSON configuration
func (s *PushbulletSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[PushbulletConfig](configJSON)
}

// Validate checks if the configuration is valid
func (s *PushbulletSender) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*PushbulletConfig))
}

// Send sends a notification via Pushbullet
func (s *PushbulletSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	m *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	cfg := cfgAny.(*PushbulletConfig)

	// Prepare notification title and body
	title := s.buildTitle(m, heartbeat)
	body := s.buildBody(cfg, message, m, heartbeat)

	// Create the push notification payload
	payload := pushbulletPayload(cfg, title, body)

	// Marshal payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Pushbullet payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.pushbullet.com/v2/pushes", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create Pushbullet request: %w", err)
	}

	// Set headers
	req.Header.Set("Access-Token", cfg.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Pushbullet notification: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return pushbulletStatusError(resp)
	}

	s.logger.Infof("Pushbullet notification sent successfully")
	return nil
}

// pushbulletPayload creates the push notification payload, with the device and channel if set
func pushbulletPayload(cfg *PushbulletConfig, title, body string) map[string]interface{} {
	payload := map[string]interface{}{
		"type":  "note",
		"title": title,
		"body":  body,
	}

	// Add optional device ID
	if cfg.DeviceID != "" {
		payload["device_iden"] = cfg.DeviceID
	}

	// Add optional channel tag
	if cfg.ChannelTag != "" {
		payload["channel_tag"] = cfg.ChannelTag
	}
	return payload
}

// pushbulletStatusError is the error for a response that is not a success: the reason the API
// gives when its body has one
func pushbulletStatusError(resp *http.Response) error {
	var respBody map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err == nil {
		if errMsg, ok := respBody["error"].(map[string]interface{}); ok {
			if msg, ok := errMsg["message"].(string); ok {
				return fmt.Errorf("pushbullet API error: %s (status: %d)", msg, resp.StatusCode)
			}
		}
	}
	return fmt.Errorf("pushbullet API returned status: %d", resp.StatusCode)
}

// pushbulletStatusName is the name of a monitor's status, or UNKNOWN
func pushbulletStatusName(status shared.MonitorStatus) string {
	switch status {
	case 0:
		return "DOWN"
	case 1:
		return "UP"
	case 2:
		return "PENDING"
	case 3:
		return "MAINTENANCE"
	}
	return "UNKNOWN"
}

// buildTitle creates the notification title
func (s *PushbulletSender) buildTitle(m *monitor.Model, heartbeat *heartbeat.Model) string {
	if m == nil {
		return "[PeekaPing] Alert"
	}

	status := "UNKNOWN"
	if heartbeat != nil {
		status = pushbulletStatusName(heartbeat.Status)
	}

	return fmt.Sprintf("[PeekaPing] %s is %s", m.Name, status)
}

// buildBody creates the notification body
func (s *PushbulletSender) buildBody(cfg *PushbulletConfig, message string, m *monitor.Model, heartbeat *heartbeat.Model) string {
	// If custom template is provided, use it
	if cfg.CustomTemplate != "" {
		engine := liquid.NewEngine()
		bindings := PrepareTemplateBindings(m, heartbeat, message)
		if rendered, err := engine.ParseAndRenderString(cfg.CustomTemplate, bindings); err == nil {
			return rendered
		}
	}

	// Default body format
	var body string
	if message != "" {
		body = message + "\n\n"
	}

	if m != nil {
		body += fmt.Sprintf("Monitor: %s\n", m.Name)
	}

	if heartbeat != nil {
		body += pushbulletHeartbeatLines(heartbeat)
	}

	return body
}

// pushbulletHeartbeatLines are the lines of the body about the heartbeat: its status, response time
// if it has one, and time
func pushbulletHeartbeatLines(heartbeat *heartbeat.Model) string {
	lines := fmt.Sprintf("Status: %s\n", pushbulletStatusName(heartbeat.Status))

	if heartbeat.Ping > 0 {
		lines += fmt.Sprintf("Response Time: %dms\n", heartbeat.Ping)
	}

	return lines + fmt.Sprintf("Time: %s", heartbeat.Time.Format("2006-01-02 15:04:05 MST"))
}
