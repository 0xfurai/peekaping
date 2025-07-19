package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"peekaping/src/modules/shared"
	"peekaping/src/version"
	"time"

	"go.uber.org/zap"
)

// OpsgenieConfig holds the configuration for Opsgenie notifications
type OpsgenieConfig struct {
	Region   string `json:"region" validate:"required,oneof=us eu"`
	ApiKey   string `json:"api_key" validate:"required"`
	Priority int    `json:"priority" validate:"omitempty,min=1,max=5"`
}

// OpsgenieSender handles sending notifications to Opsgenie
type OpsgenieSender struct {
	logger *zap.SugaredLogger
	client *http.Client
}

// NewOpsgenieSender creates a new OpsgenieSender
func NewOpsgenieSender(logger *zap.SugaredLogger) *OpsgenieSender {
	return &OpsgenieSender{
		logger: logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (o *OpsgenieSender) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[OpsgenieConfig](configJSON)
}

func (o *OpsgenieSender) Validate(configJSON string) error {
	cfg, err := o.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*OpsgenieConfig))
}

// getOpsgenieURL returns the appropriate Opsgenie API URL based on region
func (o *OpsgenieSender) getOpsgenieURL(region string) string {
	switch region {
	case "us":
		return "https://api.opsgenie.com/v2/alerts"
	case "eu":
		return "https://api.eu.opsgenie.com/v2/alerts"
	default:
		return "https://api.opsgenie.com/v2/alerts"
	}
}

// getPriority converts priority string to Opsgenie priority format
func (o *OpsgenieSender) getPriority(priority string) string {
	if priority == "" {
		return "P3" // Default priority
	}
	return fmt.Sprintf("P%s", priority)
}

// Send sends a notification to Opsgenie
func (o *OpsgenieSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	monitor *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	cfgAny, err := o.Unmarshal(configJSON)
	if err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	cfg := cfgAny.(*OpsgenieConfig)

	baseURL := o.getOpsgenieURL(cfg.Region)
	textMsg := "Peekaping Alert"

	// Handle test notification (when heartbeat is nil)
	if heartbeat == nil {
		return o.sendTestNotification(ctx, cfg, baseURL, message)
	}

	// Handle status changes
	switch heartbeat.Status {
	case shared.MonitorStatusDown:
		return o.sendDownAlert(ctx, cfg, baseURL, message, monitor, heartbeat, textMsg)
	case shared.MonitorStatusUp:
		return o.sendUpAlert(ctx, cfg, baseURL, message, monitor, heartbeat)
	default:
		o.logger.Warnf("Unknown heartbeat status: %d", heartbeat.Status)
		return nil
	}
}

// sendTestNotification sends a test notification
func (o *OpsgenieSender) sendTestNotification(ctx context.Context, cfg *OpsgenieConfig, baseURL, message string) error {
	data := map[string]any{
		"message":  message,
		"alias":    "peekaping-notification-test",
		"source":   "Peekaping",
		"priority": "P5",
	}

	return o.postToOpsgenie(ctx, cfg, baseURL, data)
}

// sendDownAlert sends an alert when monitor is down
func (o *OpsgenieSender) sendDownAlert(ctx context.Context, cfg *OpsgenieConfig, baseURL, message string, monitor *monitor.Model, heartbeat *heartbeat.Model, textMsg string) error {
	monitorName := "Unknown Monitor"
	if monitor != nil {
		monitorName = monitor.Name
	}

	data := map[string]any{
		"message":     fmt.Sprintf("%s: %s", textMsg, monitorName),
		"alias":       monitorName,
		"description": message,
		"source":      "Peekaping",
		"priority":    o.getPriority(fmt.Sprintf("%d", cfg.Priority)),
	}

	return o.postToOpsgenie(ctx, cfg, baseURL, data)
}

// sendUpAlert closes an alert when monitor is up
func (o *OpsgenieSender) sendUpAlert(ctx context.Context, cfg *OpsgenieConfig, baseURL, message string, monitor *monitor.Model, heartbeat *heartbeat.Model) error {
	if monitor == nil {
		return fmt.Errorf("monitor is nil, cannot close alert")
	}

	// Create close URL
	closeURL := fmt.Sprintf("%s/%s/close?identifierType=alias", baseURL, url.QueryEscape(monitor.Name))

	data := map[string]any{
		"source": "Peekaping",
	}

	return o.postToOpsgenie(ctx, cfg, closeURL, data)
}

// postToOpsgenie makes a POST request to Opsgenie API
func (o *OpsgenieSender) postToOpsgenie(ctx context.Context, cfg *OpsgenieConfig, url string, data map[string]any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("GenieKey %s", cfg.ApiKey))
	req.Header.Set("User-Agent", "Peekaping-Opsgenie/"+version.Version)

	o.logger.Debugf("Sending Opsgenie request to: %s", url)
	o.logger.Debugf("Request body: %s", string(jsonData))

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to Opsgenie: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Opsgenie API returned status code %d", resp.StatusCode)
	}

	o.logger.Infof("Successfully sent Opsgenie notification, status: %d", resp.StatusCode)
	return nil
}
