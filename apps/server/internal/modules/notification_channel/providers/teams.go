package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"peekaping/internal/config"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/version"
	"strings"
	"time"

	"go.uber.org/zap"
)

type TeamsConfig struct {
	WebhookURL string `json:"webhook_url" validate:"required,url"`
	ServerURL  string `json:"server_url" validate:"omitempty,url"`
}

type TeamsSender struct {
	logger *zap.SugaredLogger
	config *config.Config
}

// NewTeamsSender creates a TeamsSender
func NewTeamsSender(logger *zap.SugaredLogger, config *config.Config) *TeamsSender {
	return &TeamsSender{logger: logger, config: config}
}

func (s *TeamsSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[TeamsConfig](configJSON)
}

func (s *TeamsSender) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*TeamsConfig))
}

// getMonitorURL extracts the URL from monitor config
func (s *TeamsSender) getMonitorURL(monitor *monitor.Model) string {
	if monitor == nil {
		return ""
	}

	// Try to extract URL from monitor config JSON
	if monitor.Config != "" {
		var config map[string]any
		if err := json.Unmarshal([]byte(monitor.Config), &config); err == nil {
			// Handle different monitor types
			switch monitor.Type {
			case "http", "http-keyword", "http-json-query":
				// HTTP monitors have a 'url' field
				if url, ok := config["url"].(string); ok && url != "" {
					return url
				}
			case "tcp":
				// TCP monitors have 'host' and 'port' fields
				if hostname, ok := config["host"].(string); ok && hostname != "" {
					if port, ok := config["port"].(float64); ok && port > 0 {
						return fmt.Sprintf("%s:%.0f", hostname, port)
					}
					return hostname
				}
			case "ping":
				// Ping monitors have a 'host' field
				if hostname, ok := config["host"].(string); ok && hostname != "" {
					return hostname
				}
			case "dns":
				// DNS monitors have a 'host' field
				if hostname, ok := config["host"].(string); ok && hostname != "" {
					return hostname
				}
			}
		}
	}

	return ""
}

// getServerURL returns the server URL to use for links
func (s *TeamsSender) getServerURL(cfg *TeamsConfig) string {
	if cfg.ServerURL != "" {
		return strings.TrimRight(cfg.ServerURL, "/")
	}
	if s.config != nil && s.config.ClientURL != "" {
		return strings.TrimRight(s.config.ClientURL, "/")
	}
	return ""
}

// getStatusStyle returns the adaptive card style based on heartbeat status
func (s *TeamsSender) getStatusStyle(status shared.MonitorStatus) string {
	switch status {
	case shared.MonitorStatusUp:
		return "good"
	case shared.MonitorStatusDown:
		return "attention"
	case shared.MonitorStatusPending:
		return "default"
	case shared.MonitorStatusMaintenance:
		return "default"
	default:
		return "default"
	}
}

// buildAdaptiveCard builds the Microsoft Teams adaptive card
func (s *TeamsSender) buildAdaptiveCard(
	cfg *TeamsConfig,
	monitor *monitor.Model,
	heartbeat *heartbeat.Model,
	message string,
) map[string]any {
	serverURL := s.getServerURL(cfg)
	monitorURL := s.getMonitorURL(monitor)

	// Determine status style
	statusStyle := "default"
	if heartbeat != nil {
		statusStyle = s.getStatusStyle(heartbeat.Status)
	}

	// Build title text
	titleText := "[Peekaping] Alert"
	if monitor != nil && monitor.Name != "" {
		titleText = fmt.Sprintf("[%s] Alert", monitor.Name)
	}

	// Build description - use message parameter (which corresponds to {{ description }} in template)
	description := message
	if description == "" && heartbeat != nil {
		description = heartbeat.Msg
	}

	// Build facts - match exact order from template
	facts := []map[string]string{
		{"title": "Description", "value": description},
	}

	if monitor != nil {
		facts = append(facts, map[string]string{
			"title": "Monitor",
			"value": monitor.Name,
		})
	}

	if monitorURL != "" {
		facts = append(facts, map[string]string{
			"title": "URL",
			"value": fmt.Sprintf("[%s](%s)", monitorURL, monitorURL),
		})
	}

	if heartbeat != nil {
		// Format time similar to heartbeat.created_at (using Time field formatted as string)
		timeValue := heartbeat.Time.Format("2006-01-02 15:04:05")
		facts = append(facts, map[string]string{
			"title": "Time",
			"value": timeValue,
		})
	}

	// Build actions
	actions := []map[string]any{}

	if serverURL != "" && monitor != nil {
		peekapingURL := fmt.Sprintf("%s/monitors/%s", serverURL, monitor.ID)
		actions = append(actions, map[string]any{
			"type":  "Action.OpenUrl",
			"title": "Peekaping",
			"url":   peekapingURL,
		})
	}

	if monitorURL != "" {
		actions = append(actions, map[string]any{
			"type":  "Action.OpenUrl",
			"title": monitor.Name,
			"url":   monitorURL,
		})
	}

	// Build adaptive card
	card := map[string]any{
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"type":    "AdaptiveCard",
		"version": "1.4",
		"body": []map[string]any{
			{
				"type":  "Container",
				"style": statusStyle,
				"bleed": true,
				"items": []map[string]any{
					{
						"type": "ColumnSet",
						"columns": []map[string]any{
							{
								"type":  "Column",
								"width": "auto",
								"items": []map[string]any{
									{
										"type":      "Image",
										"url":       "https://peekaping.com/logo-mascot.webp",
										"size":      "Small",
										"style":     "Person",
										"altText":   "Peekaping",
									},
								},
							},
							{
								"type":  "Column",
								"width": "stretch",
								"items": []map[string]any{
									{
										"type":   "TextBlock",
										"text":   titleText,
										"weight": "Bolder",
										"size":   "Medium",
										"wrap":   true,
									},
									{
										"type":     "TextBlock",
										"text":     "Peekaping Alert",
										"isSubtle": true,
										"spacing":  "None",
									},
								},
							},
						},
					},
				},
			},
			{
				"type": "FactSet",
				"facts": facts,
			},
		},
	}

	if len(actions) > 0 {
		card["actions"] = actions
	}

	return card
}

func (s *TeamsSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	monitor *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	cfg := cfgAny.(*TeamsConfig)

	s.logger.Infof("Sending Teams message: %s", message)

	// Build adaptive card
	card := s.buildAdaptiveCard(cfg, monitor, heartbeat, message)

	// Create Teams message payload with adaptive card attachment
	payload := map[string]any{
		"type": "message",
		"attachments": []map[string]any{
			{
				"contentType": "application/vnd.microsoft.card.adaptive",
				"content":     card,
			},
		},
	}

	// Convert payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Teams payload: %w", err)
	}

	s.logger.Debugf("Teams payload: %s", string(jsonPayload))

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.WebhookURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping-Teams/"+version.Version)

	s.logger.Debugf("Sending Teams webhook request: %s", req.URL.String())

	// Send request
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Teams webhook: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Teams webhook returned status: %s", resp.Status)
	}

	s.logger.Infof("Teams message sent successfully")
	return nil
}

