package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"peekaping/internal/config"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/version"
	"regexp"
	"strings"

	"go.uber.org/zap"
)

type TeamsConfig struct {
	WebhookURL string `json:"webhook_url" validate:"required,url" secret:"true" endpoint:"true"`
	ServerURL  string `json:"server_url" validate:"omitempty,url"`
}

// teamsLogoURL is the card's header image. Teams card images support PNG, JPEG and GIF, not WebP.
const teamsLogoURL = "https://peekaping.com/logo-mascot.png"

// teamsErrorCode matches the codes in Power Automate error bodies, {"error": {"code": "..."}}
var teamsErrorCode = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.]{0,63}$`)

type TeamsSender struct {
	logger *zap.SugaredLogger
	config *config.Config
	client *http.Client
}

// NewTeamsSender creates a TeamsSender
func NewTeamsSender(logger *zap.SugaredLogger, config *config.Config, client *http.Client) *TeamsSender {
	return &TeamsSender{
		logger: logger,
		config: config,
		client: client,
	}
}

func (s *TeamsSender) Unmarshal(configJSON string) (any, error) {
	cfg, err := GenericUnmarshal[TeamsConfig](configJSON)
	if err != nil {
		return nil, err
	}
	// Pasted URLs often end with a newline
	cfg.WebhookURL = strings.TrimSpace(cfg.WebhookURL)
	cfg.ServerURL = strings.TrimSpace(cfg.ServerURL)
	return cfg, nil
}

func (s *TeamsSender) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*TeamsConfig))
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

// getTitle returns the card title and the container style that colors it
func (s *TeamsSender) getTitle(monitor *monitor.Model, heartbeat *heartbeat.Model) (string, string) {
	name := "Peekaping"
	if monitor != nil && monitor.Name != "" {
		name = monitor.Name
	}

	// Certificate expiry notifications carry no heartbeat
	if heartbeat == nil {
		return name, "warning"
	}

	switch heartbeat.Status {
	case shared.MonitorStatusUp:
		return fmt.Sprintf("✅ %s is up", name), "good"
	case shared.MonitorStatusDown:
		return fmt.Sprintf("🔴 %s is down", name), "attention"
	case shared.MonitorStatusPending:
		return fmt.Sprintf("⏳ %s is pending", name), "warning"
	case shared.MonitorStatusMaintenance:
		return fmt.Sprintf("🚧 %s is under maintenance", name), "accent"
	default:
		return name, "default"
	}
}

// getAddress returns the fact title and value for what the monitor checks, plus a link
// when it is an HTTP(S) URL. The card is visible to the whole channel, so credentials,
// query and fragment are dropped from URLs.
func (s *TeamsSender) getAddress(monitor *monitor.Model) (title, value, link string) {
	address := monitorAddress(monitor)
	if address == "" {
		return "", "", ""
	}

	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		// A host or host:port, which isn't a link
		return "Host", address, ""
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return "URL", u.String(), u.String()
}

// formatText keeps a message's line breaks. Teams renders card text as markdown,
// where a single newline can fold into a space, so lines are separated by blank lines.
func formatText(message string) string {
	var lines []string
	for _, line := range strings.Split(message, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n\n")
}

// buildAdaptiveCard builds the Microsoft Teams adaptive card
func (s *TeamsSender) buildAdaptiveCard(
	cfg *TeamsConfig,
	monitor *monitor.Model,
	heartbeat *heartbeat.Model,
	message string,
) map[string]any {
	title, style := s.getTitle(monitor, heartbeat)

	body := []map[string]any{
		{
			"type":  "Container",
			"style": style,
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
									"type":    "Image",
									"url":     teamsLogoURL,
									"size":    "Small",
									"style":   "Person",
									"altText": "Peekaping",
								},
							},
						},
						{
							"type":  "Column",
							"width": "stretch",
							"items": []map[string]any{
								{
									"type":   "TextBlock",
									"text":   title,
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
	}

	if text := formatText(message); text != "" {
		body = append(body, map[string]any{
			"type": "TextBlock",
			"text": text,
			"wrap": true,
		})
	}

	facts := []map[string]string{}
	if monitor != nil && monitor.Name != "" {
		facts = append(facts, map[string]string{"title": "Monitor", "value": monitor.Name})
	}

	addressTitle, address, addressLink := s.getAddress(monitor)
	if address != "" {
		facts = append(facts, map[string]string{"title": addressTitle, "value": address})
	}

	// Test notifications have no heartbeat time
	if heartbeat != nil && !heartbeat.Time.IsZero() {
		facts = append(facts, map[string]string{
			"title": "Time",
			"value": heartbeat.Time.UTC().Format("2006-01-02 15:04:05 UTC"),
		})
	}

	if len(facts) > 0 {
		body = append(body, map[string]any{
			"type":  "FactSet",
			"facts": facts,
		})
	}

	actions := []map[string]any{}

	// Test notifications use a monitor that isn't saved, so there is no page to link to
	if link := peekapingLink(s.getServerURL(cfg), monitor); link != "" && monitor.ID != "" {
		actions = append(actions, map[string]any{
			"type":  "Action.OpenUrl",
			"title": "View in Peekaping",
			"url":   link,
		})
	}

	if addressLink != "" {
		actions = append(actions, map[string]any{
			"type":  "Action.OpenUrl",
			"title": "Visit site",
			"url":   addressLink,
		})
	}

	card := map[string]any{
		"$schema":      "http://adaptivecards.io/schemas/adaptive-card.json",
		"type":         "AdaptiveCard",
		"version":      "1.4",
		"fallbackText": title,
		"body":         body,
	}

	if len(actions) > 0 {
		card["actions"] = actions
	}

	return card
}

// powerAutomateErrorCode returns the code from a Power Automate error body, or ""
func powerAutomateErrorCode(body []byte) string {
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || !teamsErrorCode.MatchString(resp.Error.Code) {
		return ""
	}
	return resp.Error.Code
}

// withoutRequestURL drops the request URL that url.Error repeats, which would leak the webhook URL
func withoutRequestURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
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

	// Create Teams message payload with adaptive card attachment
	payload := map[string]any{
		"type": "message",
		"attachments": []map[string]any{
			{
				"contentType": "application/vnd.microsoft.card.adaptive",
				"content":     s.buildAdaptiveCard(cfg, monitor, heartbeat, message),
			},
		},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Teams payload: %w", err)
	}

	s.logger.Debugf("Teams payload: %s", string(jsonPayload))

	// The webhook URL is a secret, so it is never logged
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", withoutRequestURL(err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping-Teams/"+version.Version)

	s.logger.Infof("Sending Teams message via webhook")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Teams message: %w", withoutRequestURL(err))
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Another endpoint could echo the secret webhook URL in its body or reason phrase,
		// so only Power Automate's error code and the standard status text are kept
		status := strings.TrimSpace(fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode)))
		if code := powerAutomateErrorCode(body); code != "" {
			return fmt.Errorf("Teams webhook returned status %s: %s", status, code)
		}
		return fmt.Errorf("Teams webhook returned status %s", status)
	}

	s.logger.Infof("Teams message sent successfully")
	return nil
}
