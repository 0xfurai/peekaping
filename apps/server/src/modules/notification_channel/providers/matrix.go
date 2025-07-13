package providers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"peekaping/src/version"
	"time"

	liquid "github.com/osteele/liquid"
	"go.uber.org/zap"
)

type MatrixConfig struct {
	HomeserverURL  string `json:"homeserver_url" validate:"required,url"`
	InternalRoomID string `json:"internal_room_id" validate:"required"`
	AccessToken    string `json:"access_token" validate:"required"`
	CustomMessage  string `json:"custom_message"`
}

type MatrixSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewMatrixSender creates a new MatrixSender
func NewMatrixSender(logger *zap.SugaredLogger) *MatrixSender {
	return &MatrixSender{
		logger: logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (m *MatrixSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[MatrixConfig](configJSON)
}

func (m *MatrixSender) Validate(configJSON string) error {
	cfg, err := m.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*MatrixConfig))
}

// generateRandomString generates a random string for Matrix transaction IDs
func (m *MatrixSender) generateRandomString(size int) string {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		m.logger.Warnf("Failed to generate random bytes: %v", err)
		// Fallback to timestamp-based string
		return fmt.Sprintf("peekaping_%d", time.Now().UnixNano())
	}

	randomString := base64.URLEncoding.EncodeToString(bytes)
	if len(randomString) > size {
		randomString = randomString[:size]
	}

	return randomString
}

func (m *MatrixSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	monitor *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := m.Unmarshal(configJSON)
	if err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	cfg := cfgAny.(*MatrixConfig)

	m.logger.Infof("Sending Matrix message to room: %s", cfg.InternalRoomID)

	// Prepare message content
	finalMessage := message
	if cfg.CustomMessage != "" {
		engine := liquid.NewEngine()
		bindings := PrepareTemplateBindings(monitor, heartbeat, message)

		if rendered, err := engine.ParseAndRenderString(cfg.CustomMessage, bindings); err == nil {
			finalMessage = rendered
		} else {
			m.logger.Warnf("Failed to render custom message template: %v", err)
		}
	}

	// Generate random transaction ID
	randomString := m.generateRandomString(20)
	m.logger.Debugf("Matrix Random String: %s", randomString)

	// URL encode the room ID for path component
	roomID := url.PathEscape(cfg.InternalRoomID)
	m.logger.Debugf("Matrix Room ID: %s", roomID)

	// Prepare the Matrix message payload
	data := map[string]any{
		"msgtype": "m.text",
		"body":    finalMessage,
	}

	// Convert payload to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON payload: %w", err)
	}

	// Build the Matrix API URL
	apiURL := fmt.Sprintf("%s/_matrix/client/r0/rooms/%s/send/m.room.message/%s",
		cfg.HomeserverURL, roomID, randomString)

	// Create the HTTP request
	req, err := http.NewRequestWithContext(ctx, "PUT", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Authorization", "Bearer "+cfg.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping-Matrix/"+version.Version)

	// Send the request
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Matrix message: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Matrix API returned status code: %d", resp.StatusCode)
	}

	m.logger.Infof("Matrix message sent successfully to room: %s", cfg.InternalRoomID)
	return nil
}
