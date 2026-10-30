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

type GoogleChatConfig struct {
	WebhookURL string `json:"webhook_url" validate:"required,url" secret:"true" endpoint:"true" target:"url"`
}

type GoogleChatSender struct {
	logger    *zap.SugaredLogger
	client    *http.Client
	publicURL PublicURLSource
}

// NewGoogleChatSender creates a GoogleChatSender
func NewGoogleChatSender(logger *zap.SugaredLogger, publicURL PublicURLSource, client *http.Client) *GoogleChatSender {
	return &GoogleChatSender{
		logger:    logger,
		publicURL: publicURL,
		client:    client,
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

// googleChatTitle is the header of the card: the monitor's name and whether it went down or came
// back, or a plain title for a notification without a monitor or a heartbeat
func googleChatTitle(m *monitor.Model, hb *heartbeat.Model) string {
	if m == nil || hb == nil {
		return "Peekaping Alert"
	}
	if hb.Status == shared.MonitorStatusUp {
		return fmt.Sprintf("✅ %s is back online", m.Name)
	}
	return fmt.Sprintf("🔴 %s went down", m.Name)
}

// googleChatWidgets are the widgets of the card: the message, the time of the heartbeat if there
// is one, and a button to buttonURL if there is one
func googleChatWidgets(message string, hb *heartbeat.Model, buttonURL string) []map[string]any {
	// Always show message
	widgets := []map[string]any{
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
		widgets = append(widgets, map[string]any{
			"textParagraph": map[string]string{
				"text": fmt.Sprintf("<b>Time:</b>\n%s", timeStr),
			},
		})
	}

	if buttonURL != "" {
		widgets = append(widgets, map[string]any{
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
	return widgets
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

	g.logger.Infof("Sending Google Chat notification")

	// Google Chat message formatting: https://developers.google.com/chat/api/guides/message-formats/basic
	chatHeader := map[string]string{
		"title": googleChatTitle(m, hb),
	}

	// Add button for the monitor's page, or the Probes page for a probe, if available
	buttonURL := peekapingLink(publicURL(ctx, g.publicURL), m)
	sectionWidgets := googleChatWidgets(message, hb, buttonURL)

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
