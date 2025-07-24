package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"peekaping/src/modules/shared"
	"peekaping/src/version"
	"time"

	"go.uber.org/zap"
)

type GrafanaOncallConfig struct {
	GrafanaOncallURL string `json:"grafana_oncall_url" validate:"required,url"`
}

type GrafanaOncallSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewGrafanaOncallSender creates a GrafanaOncallSender
func NewGrafanaOncallSender(logger *zap.SugaredLogger) *GrafanaOncallSender {
	return &GrafanaOncallSender{
		logger: logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (g *GrafanaOncallSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[GrafanaOncallConfig](configJSON)
}

func (g *GrafanaOncallSender) Validate(configJSON string) error {
	cfg, err := g.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*GrafanaOncallConfig))
}

func (g *GrafanaOncallSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	monitor *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := g.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	cfg := cfgAny.(*GrafanaOncallConfig)

	g.logger.Infof("Sending Grafana OnCall notification to: %s", cfg.GrafanaOncallURL)

	var payload map[string]interface{}

	if heartbeat == nil {
		// General notification
		payload = map[string]interface{}{
			"title":   "General notification",
			"message": message,
			"state":   "alerting",
		}
	} else {
		// Monitor-specific notification
		monitorName := "Unknown Monitor"
		if monitor != nil {
			monitorName = monitor.Name
		}

		switch heartbeat.Status {
		case shared.MonitorStatusDown:
			payload = map[string]interface{}{
				"title":   fmt.Sprintf("%s is down", monitorName),
				"message": heartbeat.Msg,
				"state":   "alerting",
			}
		case shared.MonitorStatusUp:
			payload = map[string]interface{}{
				"title":   fmt.Sprintf("%s is up", monitorName),
				"message": heartbeat.Msg,
				"state":   "ok",
			}
		default:
			// For pending/maintenance states, treat as alerting
			payload = map[string]interface{}{
				"title":   fmt.Sprintf("%s status changed", monitorName),
				"message": heartbeat.Msg,
				"state":   "alerting",
			}
		}
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.GrafanaOncallURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("Peekaping/%s", version.Version))

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("grafana OnCall API returned status %d: %s", resp.StatusCode, string(body))
	}

	g.logger.Infof("Grafana OnCall notification sent successfully")
	return nil
}