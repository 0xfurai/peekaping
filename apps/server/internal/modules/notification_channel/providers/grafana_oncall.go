package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/version"
	"regexp"
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

var httpStatusRe = regexp.MustCompile(`\b(\d{3})\b`)

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

	bindings := PrepareTemplateBindings(monitor, heartbeat, message)

	// extract URL or host from parsed config
	monitorURL := ""
	monitorHost := ""
	if m, ok := bindings["monitor"].(map[string]any); ok {
		if c, ok := m["config"].(map[string]any); ok {
			if u, ok := c["url"].(string); ok {
				monitorURL = u
			}
			if h, ok := c["host"].(string); ok {
				monitorHost = h
			}
		}
	}

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

		// Enrich payload with common heartbeat details
		payload["status"]  = bindings["status"]
		payload["ping_ms"] = heartbeat.Ping
		payload["retries"] = heartbeat.Retries
		payload["time"]    = heartbeat.Time.Format(time.RFC3339)

		if monitor != nil {
			payload["monitor_id"]   = monitor.ID
			payload["monitor_type"] = monitor.Type
			payload["tags"]         = monitor.Tags

			// type-specific enrichment
			switch monitor.Type {
			case "http", "http-keyword", "http-json-query":
				// HTTP monitors: expose URL and extract status code from message
				payload["monitor_url"] = monitorURL
				if matches := httpStatusRe.FindStringSubmatch(heartbeat.Msg); len(matches) > 1 {
					payload["http_status_code"] = matches[1]
				}
			case "tcp", "ping", "dns", "grpc-keyword":
				// Host-based monitors: expose host only
				payload["monitor_host"] = monitorHost
			// DB and infra monitors (mysql, postgres, redis, mongodb, etc.):
			// DSN may contain credentials — do not expose config fields
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
