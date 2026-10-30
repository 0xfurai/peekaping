package executor

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"peekaping/internal/modules/shared"
	"peekaping/internal/netguard"
	"strconv"
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
	guarded
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

// Targets lists the broker host an MQTT check connects to
func (s *MQTTExecutor) Targets(configJSON string) ([]string, error) {
	cfg, err := GenericUnmarshal[MQTTConfig](configJSON)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(mqttBrokerURL(cfg.Hostname, cfg.Port))
	if err != nil || u.Hostname() == "" {
		return nil, nil
	}
	return []string{u.Hostname()}, nil
}

func (m *MQTTExecutor) Execute(ctx context.Context, monitor *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := m.Unmarshal(monitor.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*MQTTConfig)

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
	status, message := checkMQTTMessage(cfg, receivedMessage)
	return &Result{
		Status:    status,
		Message:   message,
		StartTime: startTime,
		EndTime:   endTime,
	}
}

// checkMQTTMessage judges the received message by the config's check type
func checkMQTTMessage(cfg *MQTTConfig, receivedMessage string) (shared.MonitorStatus, string) {
	switch cfg.CheckType {
	case "none":
		// For "none" check type, any received message is considered success
		if receivedMessage != "" {
			return shared.MonitorStatusUp, fmt.Sprintf("Topic: %s; Message received", cfg.Topic)
		}
		return shared.MonitorStatusDown, fmt.Sprintf("Topic: %s; No message received", cfg.Topic)
	case "keyword":
		return checkMQTTKeyword(cfg, receivedMessage)
	case "json-query":
		return checkMQTTJSONQuery(cfg, receivedMessage)
	default:
		return shared.MonitorStatusDown, "Unknown MQTT Check Type"
	}
}

// checkMQTTKeyword is Up when the message holds the success keyword
func checkMQTTKeyword(cfg *MQTTConfig, receivedMessage string) (shared.MonitorStatus, string) {
	// Additional safety check to prevent false positives with empty keywords
	if strings.TrimSpace(cfg.SuccessKeyword) == "" {
		return shared.MonitorStatusDown, "Success keyword is empty or invalid"
	}

	if receivedMessage != "" && strings.Contains(receivedMessage, cfg.SuccessKeyword) {
		return shared.MonitorStatusUp, fmt.Sprintf("Topic: %s; Message: %s", cfg.Topic, receivedMessage)
	}
	return shared.MonitorStatusDown, fmt.Sprintf("Message Mismatch - Topic: %s; Message: %s", cfg.Topic, receivedMessage)
}

// checkMQTTJSONQuery is Up when the JSONata expression, evaluated on the message, gives the
// expected value
func checkMQTTJSONQuery(cfg *MQTTConfig, receivedMessage string) (shared.MonitorStatus, string) {
	// Additional safety checks for json-query
	if strings.TrimSpace(cfg.JsonPath) == "" {
		return shared.MonitorStatusDown, "JSON path is empty or invalid"
	}

	if strings.TrimSpace(cfg.ExpectedValue) == "" {
		return shared.MonitorStatusDown, "Expected value is empty or invalid"
	}

	resultStr, failure := evaluateMQTTJSONata(cfg.JsonPath, receivedMessage)
	if failure != "" {
		return shared.MonitorStatusDown, failure
	}

	if resultStr == cfg.ExpectedValue {
		return shared.MonitorStatusUp, "Message received, expected value is found"
	}
	return shared.MonitorStatusDown, fmt.Sprintf("Message received but value is not equal to expected value, value was: [%s]", resultStr)
}

// evaluateMQTTJSONata evaluates the JSONata expression on the JSON message. It returns the result as
// a string, which is empty for none, or why the evaluation failed.
func evaluateMQTTJSONata(expression, receivedMessage string) (string, string) {
	var parsedMessage interface{}
	if err := json.Unmarshal([]byte(receivedMessage), &parsedMessage); err != nil {
		return "", fmt.Sprintf("Failed to parse JSON message: %v", err)
	}

	expr, err := jsonata.Compile(expression)
	if err != nil {
		return "", fmt.Sprintf("Invalid JSONata expression: %v", err)
	}

	result, err := expr.Eval(parsedMessage)
	if err != nil {
		return "", fmt.Sprintf("JSONata evaluation failed: %v", err)
	}

	var resultStr string
	if result != nil {
		resultStr = fmt.Sprintf("%v", result)
	}
	return resultStr, ""
}

// mqttBrokerURL joins hostname and port into a broker URL, adding the mqtt:// scheme when
// hostname has none. An IPv6 host is bracketed, whether or not hostname already was.
func mqttBrokerURL(hostname string, port int) string {
	scheme := "mqtt://"
	for _, s := range []string{"mqtt://", "mqtts://"} {
		if strings.HasPrefix(hostname, s) {
			scheme = s
			hostname = strings.TrimPrefix(hostname, s)
			break
		}
	}
	hostname = strings.TrimSuffix(strings.TrimPrefix(hostname, "["), "]")
	return scheme + net.JoinHostPort(hostname, strconv.Itoa(port))
}

// openConnection opens the connection to a broker through the guard. The broker URL is mqtt:// or
// mqtts:// (mqttBrokerURL writes no other), so there are no WebSocket or Unix socket brokers to
// open: the guard is asked for the broker's address, and the TLS handshake of mqtts:// follows.
func (m *MQTTExecutor) openConnection(timeout time.Duration) mqtt.OpenConnectionFunc {
	return func(uri *url.URL, options mqtt.ClientOptions) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		dialer := m.dialer(&net.Dialer{Timeout: timeout})
		switch uri.Scheme {
		case "mqtt", "tcp":
			return dialer.DialContext(ctx, "tcp", uri.Host)
		case "mqtts", "ssl", "tls", "tcps":
			return (&tls.Dialer{NetDialer: dialer, Config: options.TLSConfig}).DialContext(ctx, "tcp", uri.Host)
		}
		return nil, fmt.Errorf("%w: %s brokers", netguard.ErrBlocked, uri.Scheme)
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

	// Generate random client ID
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	clientID := fmt.Sprintf("peekaping_%x", randomBytes)

	mqttUrl := mqttBrokerURL(hostname, port)

	m.logger.Debugf("MQTT connecting to %s with client ID %s", mqttUrl, clientID)

	// Set up MQTT client options
	opts := m.clientOptions(mqttUrl, clientID, username, password, timeout)

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

	// Subscribe to topic
	if token := client.Subscribe(topic, 0, m.messageHandler(topic, messageChan)); token.Wait() && token.Error() != nil {
		client.Disconnect(100)
		return "", fmt.Errorf("MQTT subscription failed: %v", token.Error())
	}

	m.logger.Debugf("MQTT subscribed to topic %s", topic)

	return waitForMQTTMessage(ctx, client, messageChan, errorChan, timeout)
}

// clientOptions is the options of the client that connects to the broker at the URL
func (m *MQTTExecutor) clientOptions(mqttUrl, clientID, username, password string, timeout time.Duration) *mqtt.ClientOptions {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(mqttUrl)
	opts.SetClientID(clientID)
	opts.SetConnectTimeout(timeout)
	opts.SetWriteTimeout(timeout)
	opts.SetKeepAlive(60 * time.Second)
	opts.SetCleanSession(true)
	if m.on() {
		// paho opens the connection itself, and would send it through the all_proxy of the
		// environment, whose far end the guard can't see. Open it here instead, through the guard.
		opts.SetCustomOpenConnectionFn(m.openConnection(timeout))
	}

	if username != "" {
		opts.SetUsername(username)
	}
	if password != "" {
		opts.SetPassword(password)
	}
	return opts
}

// messageHandler passes the payload of a message on the topic to the channel, unless it is full
func (m *MQTTExecutor) messageHandler(topic string, messageChan chan string) mqtt.MessageHandler {
	return func(client mqtt.Client, msg mqtt.Message) {
		m.logger.Debugf("MQTT message received on topic %s", msg.Topic())
		if msg.Topic() == topic {
			select {
			case messageChan <- string(msg.Payload()):
			default:
				// Channel full, ignore
			}
		}
	}
}

// waitForMQTTMessage waits for the message, and disconnects the client once it has it, or when the
// wait times out or is cancelled
func waitForMQTTMessage(ctx context.Context, client mqtt.Client, messageChan chan string, errorChan chan error, timeout time.Duration) (string, error) {
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
