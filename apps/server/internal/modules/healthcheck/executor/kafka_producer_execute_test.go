//go:build !slim

package executor

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"peekaping/internal/modules/shared"

	"github.com/IBM/sarama"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// startMockKafka starts an in-process broker that is the leader of partition 0 of "orders". It
// answers produce requests as produceResponse says, or not at all when that is nil.
func startMockKafka(t *testing.T, produceResponse sarama.MockResponse) *sarama.MockBroker {
	t.Helper()
	broker := sarama.NewMockBroker(t, 1)
	t.Cleanup(broker.Close)
	handlers := map[string]sarama.MockResponse{
		"MetadataRequest": sarama.NewMockMetadataResponse(t).
			SetBroker(broker.Addr(), broker.BrokerID()).
			SetLeader("orders", 0, broker.BrokerID()),
	}
	if produceResponse != nil {
		handlers["ProduceRequest"] = produceResponse
	}
	broker.SetHandlerByMap(handlers)
	return broker
}

func TestKafkaProducerExecutor_Execute_SendsTheMessage(t *testing.T) {
	broker := startMockKafka(t, sarama.NewMockProduceResponse(t))

	result := NewKafkaProducerExecutor(zap.NewNop().Sugar()).Execute(context.Background(), kafkaMonitor(t, 5, KafkaProducerConfig{
		Brokers:     []string{broker.Addr()},
		Topic:       "orders",
		Message:     "ping",
		SASLOptions: KafkaProducerSASLOptions{Mechanism: "None"},
	}), nil)

	assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
	assert.Equal(t, "Message sent successfully to topic 'orders' (partition: 0, offset: 0)", result.Message)
	assert.False(t, result.EndTime.Before(result.StartTime))
}

func TestKafkaProducerExecutor_Execute_AutoTopicCreationFollowsTheConfig(t *testing.T) {
	for _, allow := range []bool{false, true} {
		broker := startMockKafka(t, sarama.NewMockProduceResponse(t))

		result := NewKafkaProducerExecutor(zap.NewNop().Sugar()).Execute(context.Background(), kafkaMonitor(t, 5, KafkaProducerConfig{
			Brokers:                []string{broker.Addr()},
			Topic:                  "missing",
			Message:                "ping",
			AllowAutoTopicCreation: allow,
		}), nil)

		require.Equal(t, shared.MonitorStatusDown, result.Status, result.Message)
		var asked []bool
		for _, rr := range broker.History() {
			if metadata, ok := rr.Request.(*sarama.MetadataRequest); ok && len(metadata.Topics) > 0 {
				asked = append(asked, metadata.AllowAutoTopicCreation)
			}
		}
		require.NotEmpty(t, asked)
		for _, got := range asked {
			assert.Equal(t, allow, got)
		}
	}
}

func TestKafkaProducerExecutor_Execute_BrokerRejectsTheMessage(t *testing.T) {
	broker := startMockKafka(t, sarama.NewMockProduceResponse(t).SetError("orders", 0, sarama.ErrMessageSizeTooLarge))

	result := NewKafkaProducerExecutor(zap.NewNop().Sugar()).Execute(context.Background(), kafkaMonitor(t, 5, KafkaProducerConfig{
		Brokers: []string{broker.Addr()},
		Topic:   "orders",
		Message: "ping",
	}), nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Equal(t, "Failed to send message: kafka server: Message was too large, server rejected it to avoid allocation error", result.Message)
}

func TestKafkaProducerExecutor_Execute_BrokerNeverAcknowledges(t *testing.T) {
	broker := startMockKafka(t, nil)

	start := time.Now()
	result := NewKafkaProducerExecutor(zap.NewNop().Sugar()).Execute(context.Background(), kafkaMonitor(t, 1, KafkaProducerConfig{
		Brokers: []string{broker.Addr()},
		Topic:   "orders",
		Message: "ping",
	}), nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Equal(t, "Message send timeout after 1s", result.Message)
	assert.Less(t, time.Since(start), 4*time.Second)
}

// firstKafkaRequest is the start of what a client sends a broker first: the API key and the client
// ID of the request, and its mechanism when it is a SASL handshake.
type firstKafkaRequest struct {
	firstByte byte
	apiKey    int16
	clientID  string
	mechanism string
}

// startKafkaListener accepts one connection, reads the client's first request and closes it. A TLS
// client's first bytes are a ClientHello, which it reports as the first byte only.
func startKafkaListener(t *testing.T) (string, <-chan firstKafkaRequest) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })
	requests := make(chan firstKafkaRequest, 1)

	go func() {
		conn, err := lis.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		var first [1]byte
		if _, err := io.ReadFull(conn, first[:]); err != nil {
			return
		}
		if first[0] == 0x16 {
			requests <- firstKafkaRequest{firstByte: first[0]}
			return
		}

		// size (4), api key (2), api version (2), correlation id (4), client id (2 + n), body
		var rest [3]byte
		if _, err := io.ReadFull(conn, rest[:]); err != nil {
			return
		}
		size := binary.BigEndian.Uint32(append([]byte{first[0]}, rest[:]...))
		frame := make([]byte, size)
		if _, err := io.ReadFull(conn, frame); err != nil {
			return
		}
		request := firstKafkaRequest{firstByte: first[0], apiKey: int16(binary.BigEndian.Uint16(frame[0:2]))}
		clientIDLen := int(binary.BigEndian.Uint16(frame[8:10]))
		request.clientID = string(frame[10 : 10+clientIDLen])
		body := frame[10+clientIDLen:]
		if request.apiKey == 17 && len(body) >= 2 { // SaslHandshake
			mechanismLen := int(binary.BigEndian.Uint16(body[0:2]))
			request.mechanism = string(body[2 : 2+mechanismLen])
		}
		requests <- request
	}()

	return lis.Addr().String(), requests
}

func TestKafkaProducerExecutor_Execute_ConnectionSettings(t *testing.T) {
	const saslHandshake = 17

	tests := []struct {
		name          string
		edit          func(*KafkaProducerConfig)
		wantFirstByte byte
		wantMechanism string
	}{
		{name: "PLAIN", edit: func(c *KafkaProducerConfig) {
			c.SASLOptions = KafkaProducerSASLOptions{Mechanism: "PLAIN", Username: "u", Password: "p"}
		}, wantMechanism: "PLAIN"},
		{name: "SCRAM-SHA-256", edit: func(c *KafkaProducerConfig) {
			c.SASLOptions = KafkaProducerSASLOptions{Mechanism: "SCRAM-SHA-256", Username: "u", Password: "p"}
		}, wantMechanism: "SCRAM-SHA-256"},
		{name: "SCRAM-SHA-512", edit: func(c *KafkaProducerConfig) {
			c.SASLOptions = KafkaProducerSASLOptions{Mechanism: "SCRAM-SHA-512", Username: "u", Password: "p"}
		}, wantMechanism: "SCRAM-SHA-512"},
		{name: "SSL", edit: func(c *KafkaProducerConfig) { c.SSL = true }, wantFirstByte: 0x16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, requests := startKafkaListener(t)
			cfg := KafkaProducerConfig{Brokers: []string{addr}, Topic: "orders", Message: "ping"}
			tt.edit(&cfg)

			result := NewKafkaProducerExecutor(zap.NewNop().Sugar()).Execute(context.Background(), kafkaMonitor(t, 1, cfg), nil)

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Contains(t, result.Message, "Failed to create Kafka producer")
			select {
			case request := <-requests:
				if tt.wantFirstByte != 0 {
					assert.Equal(t, tt.wantFirstByte, request.firstByte)
					return
				}
				assert.Equal(t, int16(saslHandshake), request.apiKey)
				assert.Equal(t, tt.wantMechanism, request.mechanism)
				assert.Equal(t, "peekaping-monitor-kafka-test", request.clientID)
			case <-time.After(5 * time.Second):
				t.Fatal("the broker got no request")
			}
		})
	}
}

func TestKafkaProducerExecutor_Execute_ClientIDNamesTheMonitor(t *testing.T) {
	addr, requests := startKafkaListener(t)
	cfg := KafkaProducerConfig{Brokers: []string{addr}, Topic: "orders", Message: "ping"}

	NewKafkaProducerExecutor(zap.NewNop().Sugar()).Execute(context.Background(), kafkaMonitor(t, 1, cfg), nil)

	select {
	case request := <-requests:
		assert.Equal(t, "peekaping-monitor-kafka-test", request.clientID)
	case <-time.After(5 * time.Second):
		t.Fatal("the broker got no request")
	}
}
