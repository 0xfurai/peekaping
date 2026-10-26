//go:build !slim

package executor

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func kafkaMonitor(t *testing.T, timeout int, cfg KafkaProducerConfig) *Monitor {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return &Monitor{
		ID:      "kafka-test",
		Name:    "Kafka test",
		Type:    "kafka-producer",
		Timeout: timeout,
		Config:  string(raw),
	}
}

func TestKafkaProducerExecutor_BrokerRefusesConnection(t *testing.T) {
	// Listen to get a free port, then close the listener so the port refuses.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())

	exec := NewKafkaProducerExecutor(zap.NewNop().Sugar())
	result := exec.Execute(context.Background(), kafkaMonitor(t, 5, KafkaProducerConfig{
		Brokers: []string{addr},
		Topic:   "refused",
		Message: "ping",
	}), nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "Failed to create Kafka producer")
	assert.Contains(t, result.Message, "dial tcp "+addr+": connect: connection refused")
}

func TestKafkaProducerExecutor_BrokerNeverAnswers(t *testing.T) {
	// A broker that accepts the connection and then says nothing.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})

	exec := NewKafkaProducerExecutor(zap.NewNop().Sugar())
	start := time.Now()
	result := exec.Execute(context.Background(), kafkaMonitor(t, 2, KafkaProducerConfig{
		Brokers: []string{l.Addr().String()},
		Topic:   "silent",
		Message: "ping",
	}), nil)
	elapsed := time.Since(start)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "timed out")
	assert.Less(t, elapsed, 3*time.Second, "the check must end within the monitor's timeout plus 1s")
}

// The race between sarama's deadline and the check's, which the test above can
// hit either way, played out case by case.
func TestProducerCreationFailure(t *testing.T) {
	ioTimeout := &net.OpError{Op: "read", Net: "tcp", Err: timeoutError{}}
	refused := errors.New("kafka: client has run out of available brokers to talk to: dial tcp 127.0.0.1:1: connect: connection refused")

	running := context.Background()
	ended, cancelEnded := context.WithCancel(context.Background())
	cancelEnded()

	tests := []struct {
		name          string
		ctx, checkCtx context.Context
		err           error
		want          string
	}{
		{"the check's deadline fired first", running, ended, nil, "timed out after 2s"},
		{"sarama's read deadline fired first", running, running, ioTimeout, "timed out after 2s"},
		{"sarama failed while the check was ending", running, ended, refused, "timed out after 2s"},
		{"the caller cancelled the check", ended, ended, nil, "context canceled"},
		{"the caller cancelled as sarama failed", ended, ended, ioTimeout, "context canceled"},
		{"the broker refused the connection", running, running, refused, refused.Error()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, producerCreationFailure(tt.ctx, tt.checkCtx, tt.err, 2))
		})
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestKafkaProducerExecutor_Validate(t *testing.T) {
	exec := NewKafkaProducerExecutor(zap.NewNop().Sugar())

	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{"valid", `{"brokers":["localhost:9092"],"topic":"t","message":"m","sasl_options":{"mechanism":"None"}}`, ""},
		{"valid with SASL", `{"brokers":["localhost:9092"],"topic":"t","message":"m","sasl_options":{"mechanism":"PLAIN","username":"u","password":"p"}}`, ""},
		{"not JSON", `not json`, "invalid character"},
		{"no brokers", `{"brokers":[],"topic":"t","message":"m","sasl_options":{"mechanism":"None"}}`, "brokers list cannot be empty"},
		{"blank broker", `{"brokers":[" "],"topic":"t","message":"m","sasl_options":{"mechanism":"None"}}`, "broker address cannot be empty"},
		{"broker without port", `{"brokers":["localhost"],"topic":"t","message":"m","sasl_options":{"mechanism":"None"}}`, "broker address must be in host:port format: localhost"},
		{"blank topic", `{"brokers":["localhost:9092"],"topic":" ","message":"m","sasl_options":{"mechanism":"None"}}`, "topic name cannot be empty"},
		{"blank message", `{"brokers":["localhost:9092"],"topic":"t","message":" ","sasl_options":{"mechanism":"None"}}`, "message cannot be empty"},
		{"SASL without username", `{"brokers":["localhost:9092"],"topic":"t","message":"m","sasl_options":{"mechanism":"PLAIN","password":"p"}}`, "username is required when SASL mechanism is specified"},
		{"SASL without password", `{"brokers":["localhost:9092"],"topic":"t","message":"m","sasl_options":{"mechanism":"PLAIN","username":"u"}}`, "password is required when SASL mechanism is specified"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := exec.Validate(tt.config)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestKafkaProducerExecutor_UnknownSASLMechanism(t *testing.T) {
	exec := NewKafkaProducerExecutor(zap.NewNop().Sugar())
	result := exec.Execute(context.Background(), kafkaMonitor(t, 5, KafkaProducerConfig{
		Brokers:     []string{"127.0.0.1:1"},
		Topic:       "t",
		Message:     "m",
		SASLOptions: KafkaProducerSASLOptions{Mechanism: "GSSAPI", Username: "u", Password: "p"},
	}), nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Equal(t, "unsupported SASL mechanism: GSSAPI", result.Message)
}

func TestKafkaProducerExecutor_InvalidConfig(t *testing.T) {
	exec := NewKafkaProducerExecutor(zap.NewNop().Sugar())
	result := exec.Execute(context.Background(), &Monitor{ID: "kafka-test", Timeout: 5, Config: "not json"}, nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "invalid character")
}
