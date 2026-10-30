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
	guarded
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

	if err := validateKafkaBrokers(kafkaCfg.Brokers); err != nil {
		return err
	}

	// Validate topic name
	if strings.TrimSpace(kafkaCfg.Topic) == "" {
		return fmt.Errorf("topic name cannot be empty")
	}

	// Validate message content
	if strings.TrimSpace(kafkaCfg.Message) == "" {
		return fmt.Errorf("message cannot be empty")
	}

	if err := validateKafkaSASL(kafkaCfg.SASLOptions); err != nil {
		return err
	}

	return GenericValidator(kafkaCfg)
}

// validateKafkaBrokers checks that there are brokers, and that each is a host:port address
func validateKafkaBrokers(brokers []string) error {
	// Validate brokers list is not empty
	if len(brokers) == 0 {
		return fmt.Errorf("brokers list cannot be empty")
	}

	// Validate each broker address format
	for _, broker := range brokers {
		if strings.TrimSpace(broker) == "" {
			return fmt.Errorf("broker address cannot be empty")
		}
		// Basic validation for host:port format
		if !strings.Contains(broker, ":") {
			return fmt.Errorf("broker address must be in host:port format: %s", broker)
		}
	}
	return nil
}

// kafkaSASLEnabled reports whether the options name a SASL mechanism
func kafkaSASLEnabled(opts KafkaProducerSASLOptions) bool {
	return opts.Mechanism != "" && opts.Mechanism != "None"
}

// validateKafkaSASL checks that a SASL mechanism comes with a username and password
func validateKafkaSASL(opts KafkaProducerSASLOptions) error {
	if !kafkaSASLEnabled(opts) {
		return nil
	}
	if opts.Username == "" {
		return fmt.Errorf("username is required when SASL mechanism is specified")
	}
	if opts.Password == "" {
		return fmt.Errorf("password is required when SASL mechanism is specified")
	}
	return nil
}

// Targets lists the hosts of the brokers a Kafka check connects to. The brokers the cluster
// announces afterwards are not known here: they are dialed through the guard too.
func (k *KafkaProducerExecutor) Targets(configJSON string) ([]string, error) {
	cfg, err := GenericUnmarshal[KafkaProducerConfig](configJSON)
	if err != nil {
		return nil, err
	}
	var hosts []string
	for _, broker := range cfg.Brokers {
		if host := hostOf(broker); host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts, nil
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

	config, err := k.producerConfig(cfg, monitor.ID, timeout)
	if err != nil {
		return DownResult(err, startTime, time.Now().UTC())
	}

	producer, failure := k.createProducer(ctx, checkCtx, cfg.Brokers, config, monitor, startTime)
	if failure != nil {
		return failure
	}

	return k.sendMessage(checkCtx, producer, cfg, monitor, startTime)
}

// producerConfig is the sarama configuration for the check: its timeouts, the guard's dialer, TLS
// and SASL
func (k *KafkaProducerExecutor) producerConfig(cfg *KafkaProducerConfig, monitorID string, timeout time.Duration) (*sarama.Config, error) {
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

	// With a guard, sarama dials every broker through it, the ones the cluster announces in its
	// metadata included, so a guard turned on after the first dial refuses those. The dialer is the
	// one sarama makes itself. TLS is still negotiated by sarama, over the connection the dialer
	// returns.
	if k.hasGuard() {
		config.Net.Proxy.Enable = true
		config.Net.Proxy.Dialer = k.dialer(&net.Dialer{Timeout: timeout, KeepAlive: config.Net.KeepAlive})
	}

	// Configure SSL if enabled
	if cfg.SSL {
		config.Net.TLS.Enable = true
		config.Net.TLS.Config = &tls.Config{
			InsecureSkipVerify: false,
		}
	}

	// Configure SASL if specified
	if err := configureKafkaSASL(config, cfg.SASLOptions); err != nil {
		return nil, err
	}

	// Set client ID
	config.ClientID = fmt.Sprintf("peekaping-monitor-%s", monitorID)

	return config, nil
}

// kafkaSASLMechanisms sets up the sarama configuration for each supported SASL mechanism
var kafkaSASLMechanisms = map[string]func(config *sarama.Config){
	"PLAIN": func(config *sarama.Config) {
		config.Net.SASL.Mechanism = sarama.SASLTypePlaintext
	},
	"SCRAM-SHA-256": func(config *sarama.Config) {
		config.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
		config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
			return &scramClient{HashGeneratorFcn: scram.SHA256}
		}
	},
	"SCRAM-SHA-512": func(config *sarama.Config) {
		config.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
		config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
			return &scramClient{HashGeneratorFcn: scram.SHA512}
		}
	},
}

// configureKafkaSASL turns on SASL in the configuration when the options name a mechanism
func configureKafkaSASL(config *sarama.Config, opts KafkaProducerSASLOptions) error {
	if !kafkaSASLEnabled(opts) {
		return nil
	}

	config.Net.SASL.Enable = true
	config.Net.SASL.User = opts.Username
	config.Net.SASL.Password = opts.Password

	setMechanism, ok := kafkaSASLMechanisms[opts.Mechanism]
	if !ok {
		return fmt.Errorf("unsupported SASL mechanism: %s", opts.Mechanism)
	}
	setMechanism(config)
	return nil
}

// createProducer creates the producer, or returns the Down result for why it couldn't
func (k *KafkaProducerExecutor) createProducer(ctx, checkCtx context.Context, brokers []string, config *sarama.Config, monitor *Monitor, startTime time.Time) (sarama.SyncProducer, *Result) {
	// Create the producer in the background: sarama takes no context, so the
	// select below is what ends the check when the context does. The network
	// timeouts above end the goroutine soon after.
	type created struct {
		producer sarama.SyncProducer
		err      error
	}
	createDone := make(chan created, 1)
	go func() {
		p, createErr := sarama.NewSyncProducer(brokers, config)
		createDone <- created{p, createErr}
	}()

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
		return nil, &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("Failed to create Kafka producer: %s", reason),
			StartTime: startTime,
			EndTime:   time.Now().UTC(),
		}
	case c := <-createDone:
		if c.err != nil {
			k.logger.Infof("Kafka producer creation failed: %s, %s", monitor.Name, c.err.Error())
			reason := producerCreationFailure(ctx, checkCtx, c.err, monitor.Timeout)
			return nil, &Result{
				Status:    shared.MonitorStatusDown,
				Message:   fmt.Sprintf("Failed to create Kafka producer: %s", reason),
				StartTime: startTime,
				EndTime:   time.Now().UTC(),
			}
		}
		return c.producer, nil
	}
}

// sendMessage sends the check's message and closes the producer, and returns the check's result
func (k *KafkaProducerExecutor) sendMessage(checkCtx context.Context, producer sarama.SyncProducer, cfg *KafkaProducerConfig, monitor *Monitor, startTime time.Time) *Result {
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
