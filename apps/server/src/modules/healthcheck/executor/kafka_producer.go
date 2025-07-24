package executor

import (
	"context"
	"crypto/tls"
	"fmt"
	"peekaping/src/modules/shared"
	"strings"
	"time"

	"github.com/IBM/sarama"
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

	k.logger.Debugf("execute kafka producer cfg: %+v", cfg)

	startTime := time.Now().UTC()

	// Create Kafka configuration
	config := sarama.NewConfig()
	config.Producer.Return.Successes = true
	config.Producer.Return.Errors = true
	config.Producer.RequiredAcks = sarama.WaitForAll
	config.Producer.Timeout = time.Duration(monitor.Timeout) * time.Second
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
		case "SCRAM-SHA-512":
			config.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
		default:
			return DownResult(fmt.Errorf("unsupported SASL mechanism: %s", cfg.SASLOptions.Mechanism), startTime, time.Now().UTC())
		}
	}

	// Set client ID
	config.ClientID = fmt.Sprintf("peekaping-monitor-%s", monitor.ID)

	// Create producer
	producer, err := sarama.NewSyncProducer(cfg.Brokers, config)
	if err != nil {
		k.logger.Infof("Kafka producer creation failed: %s, %s", monitor.Name, err.Error())
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("Failed to create Kafka producer: %v", err),
			StartTime: startTime,
			EndTime:   time.Now().UTC(),
		}
	}
	defer func() {
		if closeErr := producer.Close(); closeErr != nil {
			k.logger.Debugf("Error closing Kafka producer: %v", closeErr)
		}
	}()

	// Create message
	message := &sarama.ProducerMessage{
		Topic: cfg.Topic,
		Value: sarama.StringEncoder(cfg.Message),
	}

	// Add timeout context for the send operation
	sendCtx, cancel := context.WithTimeout(ctx, time.Duration(monitor.Timeout)*time.Second)
	defer cancel()

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
	case <-sendCtx.Done():
		endTime := time.Now().UTC()
		k.logger.Infof("Kafka message send timeout: %s", monitor.Name)
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("Message send timeout after %ds", monitor.Timeout),
			StartTime: startTime,
			EndTime:   endTime,
		}
	case sendErr := <-sendDone:
		endTime := time.Now().UTC()

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
