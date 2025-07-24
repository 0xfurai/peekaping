package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"peekaping/src/modules/shared"
	"strings"
	"time"

	"go.uber.org/zap"
)

type RabbitMQConfig struct {
	Nodes    []string `json:"nodes" validate:"required,min=1,dive,url" example:"[\"https://node1.rabbitmq.com:15672\", \"https://node2.rabbitmq.com:15672\"]"`
	Username string   `json:"username" validate:"required" example:"admin"`
	Password string   `json:"password" validate:"required" example:"password"`
}

type RabbitMQExecutor struct {
	logger *zap.SugaredLogger
	client *http.Client
}

func NewRabbitMQExecutor(logger *zap.SugaredLogger) *RabbitMQExecutor {
	return &RabbitMQExecutor{
		logger: logger,
		client: &http.Client{},
	}
}

func (r *RabbitMQExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[RabbitMQConfig](configJSON)
}

func (r *RabbitMQExecutor) Validate(configJSON string) error {
	cfg, err := r.Unmarshal(configJSON)
	if err != nil {
		return err
	}

	rabbitCfg := cfg.(*RabbitMQConfig)

	// Validate each node URL
	for _, nodeURL := range rabbitCfg.Nodes {
		if _, err := url.Parse(nodeURL); err != nil {
			return fmt.Errorf("invalid node URL %s: %w", nodeURL, err)
		}
	}

	return GenericValidator(rabbitCfg)
}

func (r *RabbitMQExecutor) Execute(ctx context.Context, monitor *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := r.Unmarshal(monitor.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*RabbitMQConfig)

	r.logger.Debugf("execute rabbitmq cfg: %+v", cfg)

	startTime := time.Now().UTC()

	// Create a context with timeout for the entire operation
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(monitor.Timeout)*time.Second)
	defer cancel()

	// Try each node until one succeeds or all fail
	var lastError error
	for _, nodeURL := range cfg.Nodes {
		// Check if context is already cancelled/timed out
		select {
		case <-timeoutCtx.Done():
			endTime := time.Now().UTC()
			r.logger.Infof("RabbitMQ health check timed out: %s", monitor.Name)
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("Health check timed out after %ds", monitor.Timeout),
				StartTime: startTime,
				EndTime:   endTime,
			}
		default:
		}

		// Ensure trailing slash for proper URL joining
		baseURL := nodeURL
		if !strings.HasSuffix(baseURL, "/") {
			baseURL += "/"
		}

		// Construct the health check URL
		healthURL, err := url.JoinPath(baseURL, "api/health/checks/alarms/")
		if err != nil {
			r.logger.Debugf("failed to construct health URL for node %s: %v", nodeURL, err)
			lastError = fmt.Errorf("invalid URL construction for node %s: %w", nodeURL, err)
			continue
		}

		success, message, err := r.checkNode(
			timeoutCtx, // Use the timeout context instead of original context
			healthURL,
			cfg.Username,
			cfg.Password,
		)

		if success {
			endTime := time.Now().UTC()
			r.logger.Infof("RabbitMQ health check successful: %s", monitor.Name)
			return &Result{
				Status:    shared.MonitorStatusUp,
				Message:   message,
				StartTime: startTime,
				EndTime:   endTime,
			}
		}

		if err != nil {
			lastError = err
			r.logger.Debugf("RabbitMQ node %s failed: %v", nodeURL, err)
		}
	}

	endTime := time.Now().UTC()
	r.logger.Infof("All RabbitMQ nodes failed: %s, last error: %v", monitor.Name, lastError)

	message := "All RabbitMQ nodes failed"
	if lastError != nil {
		message = fmt.Sprintf("All RabbitMQ nodes failed: %v", lastError)
	}

	return &Result{
		Status:    shared.MonitorStatusDown,
		Message:   message,
		StartTime: startTime,
		EndTime:   endTime,
	}
}

func (r *RabbitMQExecutor) checkNode(ctx context.Context, healthURL, username, password string) (bool, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return false, "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set basic auth
	req.SetBasicAuth(username, password)
	req.Header.Set("Accept", "application/json")

	// Perform the request
	resp, err := r.client.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, "", fmt.Errorf("failed to read response: %w", err)
	}

	r.logger.Debugf("RabbitMQ response: status=%d, body=%s", resp.StatusCode, string(body))

	// Handle response based on status code
	switch resp.StatusCode {
	case 200:
		return true, "OK", nil
	case 503:
		// Parse error message from response
		var errorResp map[string]interface{}
		if err := json.Unmarshal(body, &errorResp); err == nil {
			if reason, ok := errorResp["reason"].(string); ok {
				return false, reason, fmt.Errorf("RabbitMQ health check failed: %s", reason)
			}
		}
		return false, "Service unavailable", fmt.Errorf("RabbitMQ health check failed: service unavailable")
	default:
		return false, fmt.Sprintf("%d - %s", resp.StatusCode, resp.Status), fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}
