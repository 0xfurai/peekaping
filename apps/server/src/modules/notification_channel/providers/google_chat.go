package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"peekaping/src/config"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"peekaping/src/modules/shared"
	"peekaping/src/version"
	"strings"
	"time"

	"go.uber.org/zap"
)

type GoogleChatConfig struct {
	WebhookURL string `json:"webhook_url" validate:"required,url"`
}

type GoogleChatSender struct {
	logger *zap.SugaredLogger
	client *http.Client
	config *config.Config
}

// NewGoogleChatSender creates a GoogleChatSender
func NewGoogleChatSender(logger *zap.SugaredLogger, config *config.Config) *GoogleChatSender {
	return &GoogleChatSender{
		logger: logger,
		config: config,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (g *GoogleChatSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[GoogleChatConfig](configJSON)
}

func (g *GoogleChatSender) Validate(configJSON string) error {
	cfg, err := g.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*GoogleChatConfig))
}

func (g *GoogleChatSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	m *monitor.Model,
	hb *heartbeat.Model,
) error {
	cfgAny, err := g.Unmarshal(configJSON)
	if err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	cfg := cfgAny.(*GoogleChatConfig)

	g.logger.Infof("Sending Google Chat notification to webhook: %s", cfg.WebhookURL)

	// Google Chat message formatting: https://developers.google.com/chat/api/guides/message-formats/basic
	chatHeader := map[string]string{
		"title": "Peekaping Alert",
	}

	if m != nil && hb != nil {
		if hb.Status == shared.MonitorStatusUp {
			chatHeader["title"] = fmt.Sprintf("✅ %s is back online", m.Name)
		} else {
			chatHeader["title"] = fmt.Sprintf("🔴 %s went down", m.Name)
		}
	}

	// Always show message
	sectionWidgets := []map[string]any{
		{
			"textParagraph": map[string]string{
				"text": fmt.Sprintf("<b>Message:</b>\n%s", message),
			},
		},
	}

	// Add time if available
	if hb != nil {
		// Format timestamp (Note: In a real implementation, you might want to add timezone support)
		timeStr := hb.Time.Format("2006-01-02 15:04:05")
		sectionWidgets = append(sectionWidgets, map[string]any{
			"textParagraph": map[string]string{
				"text": fmt.Sprintf("<b>Time:</b>\n%s", timeStr),
			},
		})
	}

	// Add button for monitor link if available
	if m != nil && g.config.ClientURL != "" {
		buttonURL := fmt.Sprintf("%s/monitors/%s", strings.TrimRight(g.config.ClientURL, "/"), m.ID)

		sectionWidgets = append(sectionWidgets, map[string]any{
			"buttonList": map[string][]map[string]any{
				"buttons": {
					{
						"text": "Visit Peekaping",
						"onClick": map[string]any{
							"openLink": map[string]string{
								"url": buttonURL,
							},
						},
					},
				},
			},
		})
	}

	chatSections := []map[string]any{
		{
			"widgets": sectionWidgets,
		},
	}

	// Construct JSON data
	data := map[string]any{
		"fallbackText": chatHeader["title"],
		"cardsV2": []map[string]any{
			{
				"card": map[string]any{
					"header":   chatHeader,
					"sections": chatSections,
				},
			},
		},
	}

	// Convert to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON payload: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.WebhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping-GoogleChat/"+version.Version)

	// Send request
	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Google Chat API returned status code: %d", resp.StatusCode)
	}

	g.logger.Infof("Google Chat notification sent successfully")
	return nil
}
