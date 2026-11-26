package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/version"
	"strings"
	"time"

	"go.uber.org/zap"
)

type GrafanaOncallConfig struct {
	GrafanaOncallURL string `json:"grafana_oncall_url" validate:"required,url" secret:"true" endpoint:"true" target:"url"`
}

type GrafanaOncallSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewGrafanaOncallSender creates a GrafanaOncallSender
func NewGrafanaOncallSender(logger *zap.SugaredLogger, client *http.Client) *GrafanaOncallSender {
	return &GrafanaOncallSender{
		logger: logger,
		client: client,
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

	g.logger.Infof("Sending Grafana OnCall notification")

	payload := g.buildPayload(message, monitor, heartbeat)
	if payload == nil {
		return nil
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

// buildPayload is the alert for a notification, or nil for a probe's notification that says
// nothing to send
func (g *GrafanaOncallSender) buildPayload(message string, m *monitor.Model, hb *heartbeat.Model) map[string]interface{} {
	var payload map[string]interface{}

	switch {
	case hb == nil && m.IsProbeEvent():
		// A probe's notification has no heartbeat, so the status of its stand-in monitor says
		// whether the probe went offline or came back online
		payload = grafanaOncallProbePayload(message, m)
		if payload == nil {
			g.logger.Warnf("Unknown probe status: %d", m.Status)
			return nil
		}
	case hb == nil:
		// General notification
		payload = map[string]interface{}{
			"title":   "General notification",
			"message": message,
			"state":   "alerting",
		}
	default:
		// Monitor-specific notification
		payload = grafanaOncallHeartbeatPayload(m, hb)
	}

	// Certificate expiry notifications come with the monitor but no heartbeat
	if m != nil {
		addGrafanaOncallMonitorFields(payload, m)
	}
	return payload
}

// grafanaOncallHeartbeatPayload is the alert for a monitor's heartbeat: alerting for Down, ok for
// Up, and alerting for any other status
func grafanaOncallHeartbeatPayload(m *monitor.Model, hb *heartbeat.Model) map[string]interface{} {
	monitorName := unknownMonitorName
	if m != nil {
		monitorName = m.Name
	}

	var title, state string
	switch hb.Status {
	case shared.MonitorStatusDown:
		title, state = fmt.Sprintf("%s is down", monitorName), "alerting"
	case shared.MonitorStatusUp:
		title, state = fmt.Sprintf("%s is up", monitorName), "ok"
	default:
		// For pending/maintenance states, treat as alerting
		title, state = fmt.Sprintf("%s status changed", monitorName), "alerting"
	}
	payload := map[string]interface{}{
		"title":   title,
		"message": hb.Msg,
		"state":   state,
	}

	// OnCall groups alerts by alert_uid and resolves a group on state ok, so the monitor's id
	// in the form of PagerDuty's dedup key keeps its Up from resolving other monitors' alerts.
	// A test notification's monitor has no ID, and so no alert group of its own.
	if m != nil && m.ID != "" {
		payload["alert_uid"] = fmt.Sprintf("Peekaping/%s", m.ID)
	}

	payload["status"] = humanReadableStatus(int(hb.Status))
	payload["ping_ms"] = hb.Ping
	payload["retries"] = hb.Retries
	// A test notification's heartbeat has no time
	if !hb.Time.IsZero() {
		payload["time"] = hb.Time.Format(time.RFC3339)
	}
	return payload
}

// grafanaOncallProbePayload is the alert for a probe that went offline, or the one that resolves it
// when the probe is back online, or nil for any other status. OnCall groups alerts by alert_uid,
// the probe's id in the form of PagerDuty's dedup key.
func grafanaOncallProbePayload(message string, probe *monitor.Model) map[string]interface{} {
	var title, state string
	switch probe.Status {
	case shared.MonitorStatusDown:
		title, state = fmt.Sprintf("%s is offline", probe.Name), "alerting"
	case shared.MonitorStatusUp:
		title, state = fmt.Sprintf("%s is online", probe.Name), "ok"
	default:
		return nil
	}

	return map[string]interface{}{
		"title":     title,
		"message":   message,
		"state":     state,
		"alert_uid": fmt.Sprintf("Peekaping/%s", probe.ID),
		"status":    humanReadableStatus(int(probe.Status)),
	}
}

// addGrafanaOncallMonitorFields adds the monitor's ID, type, tags and target to the payload,
// for OnCall to route and group alerts by.
func addGrafanaOncallMonitorFields(payload map[string]interface{}, m *monitor.Model) {
	// A test notification's monitor has no ID
	if m.ID != "" {
		payload["monitor_id"] = m.ID
	}
	payload["monitor_type"] = m.Type

	// Send [] rather than null for an untagged monitor, so OnCall templates can treat it as a list
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}
	payload["tags"] = tags

	addGrafanaOncallTarget(payload, m)
}

// addGrafanaOncallTarget adds what the monitor checks to the payload: the URL of an HTTP monitor,
// or the host of a TCP, ping, DNS or gRPC one
func addGrafanaOncallTarget(payload map[string]interface{}, m *monitor.Model) {
	var cfg struct {
		URL     string `json:"url"`
		Host    string `json:"host"`
		GrpcURL string `json:"grpcUrl"`
	}
	if m.Config == "" || json.Unmarshal([]byte(m.Config), &cfg) != nil {
		return
	}

	// Other monitor types are left out: their config, such as a database DSN, can hold credentials
	switch m.Type {
	case "http", "http-keyword", "http-json-query":
		setIfNotEmpty(payload, "monitor_url", urlWithoutSecrets(cfg.URL))
	case "tcp", "ping", "dns":
		setIfNotEmpty(payload, "monitor_host", cfg.Host)
	case "grpc-keyword":
		setIfNotEmpty(payload, "monitor_host", grpcTargetWithoutSecrets(cfg.GrpcURL))
	}
}

// setIfNotEmpty sets the payload's key to value, unless value is empty
func setIfNotEmpty(payload map[string]interface{}, key, value string) {
	if value != "" {
		payload[key] = value
	}
}

// urlWithoutSecrets returns rawURL without its userinfo, query and fragment, which can hold
// credentials or API keys. It returns "" if rawURL isn't an absolute URL with a host.
func urlWithoutSecrets(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return stripURLSecrets(u)
}

// grpcTargetWithoutSecrets returns a gRPC target without the userinfo, query and fragment of
// a URI-form target such as dns://user:pass@1.1.1.1/example.com:443. A plain host:port target
// is returned as is. It returns "" if a URI-form target doesn't parse.
func grpcTargetWithoutSecrets(target string) string {
	if !strings.Contains(target, "://") {
		return target
	}
	u, err := url.Parse(target)
	if err != nil {
		return ""
	}
	return stripURLSecrets(u)
}

func stripURLSecrets(u *url.URL) string {
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}
