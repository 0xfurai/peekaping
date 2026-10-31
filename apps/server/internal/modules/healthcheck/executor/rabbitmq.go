package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"peekaping/internal/modules/shared"
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
	guarded
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

// Targets lists the hosts of the nodes a RabbitMQ check calls
func (r *RabbitMQExecutor) Targets(configJSON string) ([]string, error) {
	cfg, err := GenericUnmarshal[RabbitMQConfig](configJSON)
	if err != nil {
		return nil, err
	}
	var hosts []string
	for _, node := range cfg.Nodes {
		if u, err := url.Parse(node); err == nil && u.Hostname() != "" {
			hosts = append(hosts, u.Hostname())
		}
	}
	return hosts, nil
}

func (r *RabbitMQExecutor) Execute(ctx context.Context, monitor *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := r.Unmarshal(monitor.Config)
	if err != nil {
		return DownResult(err, time.Now(), time.Now())
	}
	cfg := cfgAny.(*RabbitMQConfig)

	startTime := time.Now()

	// Create a context with timeout for the entire operation
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(monitor.Timeout)*time.Second)
	defer cancel()

	// With a guard, the management API is called through a transport of this check's that dials
	// through it, so a node that redirects to a blocked address is refused too, also when the guard
	// was turned on after the check started
	client := r.client
	if r.hasGuard() {
		var closeIdle func()
		client, closeIdle = guardedClient(r.guard)
		defer closeIdle()
	}

	// Try each node until one succeeds or all fail
	var lastError error
	for i, nodeURL := range cfg.Nodes {
		// Check if the whole check is already cancelled/timed out
		if timeoutCtx.Err() != nil {
			endTime := time.Now()
			r.logger.Infof("RabbitMQ health check timed out: %s", monitor.Name)
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("Health check timed out after %ds", monitor.Timeout),
				StartTime: startTime,
				EndTime:   endTime,
			}
		}

		message, err := r.tryNode(timeoutCtx, client, cfg, nodeURL, len(cfg.Nodes)-i)
		if err == nil {
			endTime := time.Now()
			r.logger.Infof("RabbitMQ health check successful: %s", monitor.Name)
			return &Result{
				Status:    shared.MonitorStatusUp,
				Message:   message,
				StartTime: startTime,
				EndTime:   endTime,
			}
		}

		lastError = err
		r.logger.Debugf("RabbitMQ node %s failed: %v", nodeURL, err)
	}

	endTime := time.Now()
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

// tryNode checks one node and returns its message, or the error that failed it. The node gets the
// check's remaining time divided by the nodes left to try (itself included), so a node that never
// answers can't use up the time of those after it, and the time a refused node leaves unused goes
// to them. The share ends with the node: it doesn't cancel the check's own context.
func (r *RabbitMQExecutor) tryNode(ctx context.Context, client *http.Client, cfg *RabbitMQConfig, nodeURL string, nodesLeft int) (string, error) {
	// Ensure trailing slash for proper URL joining
	baseURL := nodeURL
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	// Construct the health check URL
	healthURL, err := url.JoinPath(baseURL, "api/health/checks/alarms/")
	if err != nil {
		return "", fmt.Errorf("invalid URL construction for node %s: %w", nodeURL, err)
	}

	nodeCtx, cancel := context.WithTimeout(ctx, nodeShare(ctx, nodesLeft))
	defer cancel()

	success, message, err := r.checkNode(nodeCtx, client, healthURL, cfg.Username, cfg.Password)
	if success {
		return message, nil
	}
	return "", err
}

// nodeShare is the time left until ctx's deadline (the check always sets one) divided by the nodes left to try
func nodeShare(ctx context.Context, nodesLeft int) time.Duration {
	deadline, _ := ctx.Deadline()
	return time.Until(deadline) / time.Duration(nodesLeft)
}

func (r *RabbitMQExecutor) checkNode(ctx context.Context, client *http.Client, healthURL, username, password string) (bool, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return false, "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set basic auth
	req.SetBasicAuth(username, password)
	req.Header.Set("Accept", "application/json")

	// Perform the request
	resp, err := client.Do(req)
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
