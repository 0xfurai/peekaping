//go:build !slim

package executor

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"peekaping/internal/modules/shared"
	"strings"
	"time"

	"github.com/IBM/sarama"
	"github.com/xdg-go/scram"
	"go.uber.org/zap"
)

type KafkaProducerConfig struct {
	Brokers                []string                 `json:"brokers" validate:"required" example:"[\"localhost:9092\"]"`
	Topic                  string                   `json:"topic" validate:"required" example:"test-topic"`
	Message                string                   `json:"message" validate:"required" example:"{\"status\":\"up\"}"`
	AllowAutoTopicCreation bool                     `json:"allow_auto_topic_creation" example:"false"`
	SSL                    bool                     `json:"ssl" example:"false"`
	SASLOptions            KafkaProducerSASLOptions `json:"sasl_options"`
}

type KafkaProducerSASLOptions struct {
	Mechanism string `json:"mechanism" validate:"oneof=PLAIN SCRAM-SHA-256 SCRAM-SHA-512 None" example:"PLAIN"`
	Username  string `json:"username" example:"kafka_user"`
	Password  string `json:"password" example:"kafka_password"`
}

type KafkaProducerExecutor struct {
	logger *zap.SugaredLogger
}

func NewKafkaProducerExecutor(logger *zap.SugaredLogger) *KafkaProducerExecutor {
	return &KafkaProducerExecutor{
		logger: logger,
	}
}

func init() { registerOptional("kafka-producer", NewKafkaProducerExecutor) }

func (k *KafkaProducerExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[KafkaProducerConfig](configJSON)
}

func (k *KafkaProducerExecutor) Validate(configJSON string) error {
	cfg, err := k.Unmarshal(configJSON)
	if err != nil {
		return err
	}

	kafkaCfg := cfg.(*KafkaProducerConfig)

	// Validate brokers list is not empty
	if len(kafkaCfg.Brokers) == 0 {
		return fmt.Errorf("brokers list cannot be empty")
	}

	// Validate each broker address format
	for _, broker := range kafkaCfg.Brokers {
		if strings.TrimSpace(broker) == "" {
			return fmt.Errorf("broker address cannot be empty")
		}
		// Basic validation for host:port format
		if !strings.Contains(broker, ":") {
			return fmt.Errorf("broker address must be in host:port format: %s", broker)
		}
	}

	// Validate topic name
	if strings.TrimSpace(kafkaCfg.Topic) == "" {
		return fmt.Errorf("topic name cannot be empty")
	}

	// Validate message content
	if strings.TrimSpace(kafkaCfg.Message) == "" {
		return fmt.Errorf("message cannot be empty")
	}

	// Validate SASL mechanism if provided
	if kafkaCfg.SASLOptions.Mechanism != "" && kafkaCfg.SASLOptions.Mechanism != "None" {
		if kafkaCfg.SASLOptions.Username == "" {
			return fmt.Errorf("username is required when SASL mechanism is specified")
		}
		if kafkaCfg.SASLOptions.Password == "" {
			return fmt.Errorf("password is required when SASL mechanism is specified")
		}
	}

	return GenericValidator(kafkaCfg)
}

func (k *KafkaProducerExecutor) Execute(ctx context.Context, monitor *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := k.Unmarshal(monitor.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*KafkaProducerConfig)

	startTime := time.Now().UTC()

	// The monitor's timeout bounds the whole check: connecting, fetching
	// metadata and sending. sarama's own defaults are 30s per dial or read and
	// three metadata retries, which kept a broker that never answers busy for
	// two minutes.
	timeout := time.Duration(monitor.Timeout) * time.Second
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create Kafka configuration
	config := sarama.NewConfig()
	config.Producer.Return.Successes = true
	config.Producer.Return.Errors = true
	config.Producer.RequiredAcks = sarama.WaitForAll
	config.Producer.Timeout = timeout
	config.Net.DialTimeout = timeout
	config.Net.ReadTimeout = timeout
	config.Net.WriteTimeout = timeout
	// Bounds the metadata fetch with its retries. The retries stay: a topic the
	// broker has just created has no leader yet, and the next attempt finds it.
	config.Metadata.Timeout = timeout
	config.Metadata.AllowAutoTopicCreation = cfg.AllowAutoTopicCreation

	// Configure SSL if enabled
	if cfg.SSL {
		config.Net.TLS.Enable = true
		config.Net.TLS.Config = &tls.Config{
			InsecureSkipVerify: false,
		}
	}

	// Configure SASL if specified
	if cfg.SASLOptions.Mechanism != "" && cfg.SASLOptions.Mechanism != "None" {
		config.Net.SASL.Enable = true
		config.Net.SASL.User = cfg.SASLOptions.Username
		config.Net.SASL.Password = cfg.SASLOptions.Password

		switch cfg.SASLOptions.Mechanism {
		case "PLAIN":
			config.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		case "SCRAM-SHA-256":
			config.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
			config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &scramClient{HashGeneratorFcn: scram.SHA256}
			}
		case "SCRAM-SHA-512":
			config.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
			config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &scramClient{HashGeneratorFcn: scram.SHA512}
			}
		default:
			return DownResult(fmt.Errorf("unsupported SASL mechanism: %s", cfg.SASLOptions.Mechanism), startTime, time.Now().UTC())
		}
	}

	// Set client ID
	config.ClientID = fmt.Sprintf("peekaping-monitor-%s", monitor.ID)

	// Create the producer in the background: sarama takes no context, so the
	// select below is what ends the check when the context does. The network
	// timeouts above end the goroutine soon after.
	type created struct {
		producer sarama.SyncProducer
		err      error
	}
	createDone := make(chan created, 1)
	go func() {
		p, createErr := sarama.NewSyncProducer(cfg.Brokers, config)
		createDone <- created{p, createErr}
	}()

	var producer sarama.SyncProducer
	select {
	case <-checkCtx.Done():
		// A producer that arrives after the check has ended is closed.
		go func() {
			if c := <-createDone; c.producer != nil {
				k.closeProducer(c.producer)
			}
		}()
		reason := producerCreationFailure(ctx, checkCtx, nil, monitor.Timeout)
		k.logger.Infof("Kafka producer creation failed: %s, %s", monitor.Name, reason)
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("Failed to create Kafka producer: %s", reason),
			StartTime: startTime,
			EndTime:   time.Now().UTC(),
		}
	case c := <-createDone:
		if c.err != nil {
			k.logger.Infof("Kafka producer creation failed: %s, %s", monitor.Name, c.err.Error())
			reason := producerCreationFailure(ctx, checkCtx, c.err, monitor.Timeout)
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("Failed to create Kafka producer: %s", reason),
				StartTime: startTime,
				EndTime:   time.Now().UTC(),
			}
		}
		producer = c.producer
	}

	// Create message
	message := &sarama.ProducerMessage{
		Topic: cfg.Topic,
		Value: sarama.StringEncoder(cfg.Message),
	}

	// Send message with timeout handling
	var partition int32
	var offset int64
	sendDone := make(chan error, 1)

	go func() {
		var sendErr error
		partition, offset, sendErr = producer.SendMessage(message)
		sendDone <- sendErr
	}()

	select {
	case <-checkCtx.Done():
		endTime := time.Now().UTC()
		k.logger.Infof("Kafka message send timeout: %s", monitor.Name)
		// Close once the send has returned, in the background so it can't hold
		// up the result. Closing sooner could close the producer's input before
		// the send reaches it, and sarama would panic on the closed channel.
		go func() {
			<-sendDone
			k.closeProducer(producer)
		}()
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("Message send timeout after %ds", monitor.Timeout),
			StartTime: startTime,
			EndTime:   endTime,
		}
	case sendErr := <-sendDone:
		endTime := time.Now().UTC()
		k.closeProducer(producer)

		if sendErr != nil {
			k.logger.Infof("Kafka message send failed: %s, %s", monitor.Name, sendErr.Error())
			return &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("Failed to send message: %v", sendErr),
				StartTime: startTime,
				EndTime:   endTime,
			}
		}

		k.logger.Infof("Kafka message sent successfully: %s, partition: %d, offset: %d", monitor.Name, partition, offset)
		return &Result{
			Status:    shared.MonitorStatusUp,
			Message:   fmt.Sprintf("Message sent successfully to topic '%s' (partition: %d, offset: %d)", cfg.Topic, partition, offset),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}
}

// scramClient adapts xdg-go/scram to sarama's SCRAMClient. sarama needs one for
// SCRAM-SHA-256 and SCRAM-SHA-512 and has none of its own.
type scramClient struct {
	*scram.Client
	*scram.ClientConversation
	scram.HashGeneratorFcn
}

func (x *scramClient) Begin(userName, password, authzID string) error {
	client, err := x.HashGeneratorFcn.NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	x.Client = client
	x.ClientConversation = client.NewConversation()
	return nil
}

func (x *scramClient) Step(challenge string) (string, error) {
	return x.ClientConversation.Step(challenge)
}

func (x *scramClient) Done() bool {
	return x.ClientConversation.Done()
}

// producerCreationFailure says why creating the producer failed. sarama's
// network timeouts equal the check's, so against a broker that never answers
// either may fire first; both are reported as the check timing out, so the
// message doesn't depend on which deadline won. A check cancelled by its caller
// says so instead. err is nil when the check ended before sarama returned.
func producerCreationFailure(ctx, checkCtx context.Context, err error, timeout int) string {
	if ctx.Err() != nil {
		return ctx.Err().Error()
	}
	var netErr net.Error
	if err == nil || checkCtx.Err() != nil || (errors.As(err, &netErr) && netErr.Timeout()) {
		return fmt.Sprintf("timed out after %ds", timeout)
	}
	return err.Error()
}

func (k *KafkaProducerExecutor) closeProducer(producer sarama.SyncProducer) {
	if err := producer.Close(); err != nil {
		k.logger.Debugf("Error closing Kafka producer: %v", err)
	}
}
