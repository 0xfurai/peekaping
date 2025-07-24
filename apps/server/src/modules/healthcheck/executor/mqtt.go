package executor

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"peekaping/src/modules/shared"
	"strings"
	"time"

	"github.com/blues/jsonata-go"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"
)

type MQTTConfig struct {
	Hostname       string `json:"hostname" validate:"required" example:"localhost"`
	Port           int    `json:"port" validate:"required,min=1,max=65535" example:"1883"`
	Topic          string `json:"topic" validate:"required" example:"test/topic"`
	Username       string `json:"username" example:"user"`
	Password       string `json:"password" example:"password"`
	CheckType      string `json:"check_type" validate:"oneof=keyword json-query none" example:"keyword"`
	SuccessKeyword string `json:"success_keyword" example:"success"`
	JsonPath       string `json:"json_path" example:"$.status"`
	ExpectedValue  string `json:"expected_value" example:"ok"`
}

type MQTTExecutor struct {
	logger *zap.SugaredLogger
}

func NewMQTTExecutor(logger *zap.SugaredLogger) *MQTTExecutor {
	return &MQTTExecutor{
		logger: logger,
	}
}

func (s *MQTTExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[MQTTConfig](configJSON)
}

func (s *MQTTExecutor) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}

	config := cfg.(*MQTTConfig)

	// Generic validation first
	if err := GenericValidator(config); err != nil {
		return err
	}

	// Custom conditional validation
	if config.CheckType == "keyword" {
		if strings.TrimSpace(config.SuccessKeyword) == "" {
			return fmt.Errorf("success_keyword is required when check_type is 'keyword'")
		}
	} else if config.CheckType == "json-query" {
		if strings.TrimSpace(config.JsonPath) == "" {
			return fmt.Errorf("json_path is required when check_type is 'json-query'")
		}
		if strings.TrimSpace(config.ExpectedValue) == "" {
			return fmt.Errorf("expected_value is required when check_type is 'json-query'")
		}
	}
	// "none" check type requires no additional validation

	return nil
}

func (m *MQTTExecutor) Execute(ctx context.Context, monitor *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := m.Unmarshal(monitor.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*MQTTConfig)

	m.logger.Debugf("execute mqtt cfg: %+v", cfg)

	startTime := time.Now().UTC()

	// Set default check type if not specified
	if cfg.CheckType == "" {
		cfg.CheckType = "none"
	}

	// Connect to MQTT broker and receive message
	receivedMessage, err := m.mqttAsync(ctx, cfg.Hostname, cfg.Topic, map[string]interface{}{
		"port":     cfg.Port,
		"username": cfg.Username,
		"password": cfg.Password,
		"timeout":  time.Duration(monitor.Timeout) * time.Second,
	})

	endTime := time.Now().UTC()

	if err != nil {
		m.logger.Infof("MQTT connection failed: %s, %s", monitor.Name, err.Error())
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("MQTT connection failed: %v", err),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	// Perform check based on check type
	if cfg.CheckType == "none" {
		// For "none" check type, any received message is considered success
		if receivedMessage != "" {
			return &Result{
				Status:    shared.MonitorStatusUp,
				Message:   fmt.Sprintf("Topic: %s; Message received", cfg.Topic),
				StartTime: startTime,
				EndTime:   endTime,
			}
		} else {
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("Topic: %s; No message received", cfg.Topic),
				StartTime: startTime,
				EndTime:   endTime,
			}
		}
	} else if cfg.CheckType == "keyword" {
		// Additional safety check to prevent false positives with empty keywords
		if strings.TrimSpace(cfg.SuccessKeyword) == "" {
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   "Success keyword is empty or invalid",
				StartTime: startTime,
				EndTime:   endTime,
			}
		}

		if receivedMessage != "" && strings.Contains(receivedMessage, cfg.SuccessKeyword) {
			return &Result{
				Status:    shared.MonitorStatusUp,
				Message:   fmt.Sprintf("Topic: %s; Message: %s", cfg.Topic, receivedMessage),
				StartTime: startTime,
				EndTime:   endTime,
			}
		} else {
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("Message Mismatch - Topic: %s; Message: %s", cfg.Topic, receivedMessage),
				StartTime: startTime,
				EndTime:   endTime,
			}
		}
	} else if cfg.CheckType == "json-query" {
		// Additional safety checks for json-query
		if strings.TrimSpace(cfg.JsonPath) == "" {
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   "JSON path is empty or invalid",
				StartTime: startTime,
				EndTime:   endTime,
			}
		}

		if strings.TrimSpace(cfg.ExpectedValue) == "" {
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   "Expected value is empty or invalid",
				StartTime: startTime,
				EndTime:   endTime,
			}
		}
		var parsedMessage interface{}
		if err := json.Unmarshal([]byte(receivedMessage), &parsedMessage); err != nil {
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("Failed to parse JSON message: %v", err),
				StartTime: startTime,
				EndTime:   endTime,
			}
		}

		expr, err := jsonata.Compile(cfg.JsonPath)
		if err != nil {
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("Invalid JSONata expression: %v", err),
				StartTime: startTime,
				EndTime:   endTime,
			}
		}

		result, err := expr.Eval(parsedMessage)
		if err != nil {
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("JSONata evaluation failed: %v", err),
				StartTime: startTime,
				EndTime:   endTime,
			}
		}

		var resultStr string
		if result != nil {
			resultStr = fmt.Sprintf("%v", result)
		}

		if resultStr == cfg.ExpectedValue {
			return &Result{
				Status:    shared.MonitorStatusUp,
				Message:   "Message received, expected value is found",
				StartTime: startTime,
				EndTime:   endTime,
			}
		} else {
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("Message received but value is not equal to expected value, value was: [%s]", resultStr),
				StartTime: startTime,
				EndTime:   endTime,
			}
		}
	} else {
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   "Unknown MQTT Check Type",
			StartTime: startTime,
			EndTime:   endTime,
		}
	}
}

// mqttAsync connects to MQTT broker, subscribes to topic and receives message as string
func (m *MQTTExecutor) mqttAsync(ctx context.Context, hostname, topic string, options map[string]interface{}) (string, error) {
	port, _ := options["port"].(int)
	username, _ := options["username"].(string)
	password, _ := options["password"].(string)
	timeout, _ := options["timeout"].(time.Duration)

	if timeout == 0 {
		timeout = 20 * time.Second
	}

	// Add MQTT protocol to hostname if not present
	if !strings.HasPrefix(hostname, "mqtt://") && !strings.HasPrefix(hostname, "mqtts://") {
		hostname = "mqtt://" + hostname
	}

	// Generate random client ID
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	clientID := fmt.Sprintf("peekaping_%x", randomBytes)

	mqttUrl := fmt.Sprintf("%s:%d", hostname, port)

	m.logger.Debugf("MQTT connecting to %s with client ID %s", mqttUrl, clientID)

	// Set up MQTT client options
	opts := mqtt.NewClientOptions()
	opts.AddBroker(mqttUrl)
	opts.SetClientID(clientID)
	opts.SetConnectTimeout(timeout)
	opts.SetWriteTimeout(timeout)
	opts.SetKeepAlive(60 * time.Second)
	opts.SetCleanSession(true)

	if username != "" {
		opts.SetUsername(username)
	}
	if password != "" {
		opts.SetPassword(password)
	}

	// Create channel to receive message
	messageChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	// Create MQTT client
	client := mqtt.NewClient(opts)

	// Connect to broker
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return "", fmt.Errorf("MQTT connection failed: %v", token.Error())
	}

	m.logger.Debugf("MQTT connected successfully")

	// Set up message handler
	messageHandler := func(client mqtt.Client, msg mqtt.Message) {
		m.logger.Debugf("MQTT message received on topic %s", msg.Topic())
		if msg.Topic() == topic {
			select {
			case messageChan <- string(msg.Payload()):
			default:
				// Channel full, ignore
			}
		}
	}

	// Subscribe to topic
	if token := client.Subscribe(topic, 0, messageHandler); token.Wait() && token.Error() != nil {
		client.Disconnect(100)
		return "", fmt.Errorf("MQTT subscription failed: %v", token.Error())
	}

	m.logger.Debugf("MQTT subscribed to topic %s", topic)

	// Wait for message or timeout
	timeoutTimer := time.NewTimer(time.Duration(float64(timeout) * 0.8))
	defer timeoutTimer.Stop()

	select {
	case message := <-messageChan:
		client.Disconnect(100)
		return message, nil
	case err := <-errorChan:
		client.Disconnect(100)
		return "", err
	case <-timeoutTimer.C:
		client.Disconnect(100)
		return "", fmt.Errorf("timeout, message not received within %v", timeout)
	case <-ctx.Done():
		client.Disconnect(100)
		return "", fmt.Errorf("context cancelled")
	}
}
