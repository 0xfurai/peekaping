package executor

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeMQTTBroker is an MQTT 3.1.1 broker for one kind of client: it accepts a connection, and
// answers a subscription by publishing what publish holds (the topic of a message, and its payload).
type fakeMQTTBroker struct {
	port    int
	publish []mqttMessage

	mu       sync.Mutex
	username string
	password string
	clientID string
}

type mqttMessage struct{ topic, payload string }

func startFakeMQTTBroker(t *testing.T, publish ...mqttMessage) *fakeMQTTBroker {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })
	broker := &fakeMQTTBroker{port: lis.Addr().(*net.TCPAddr).Port, publish: publish}
	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			go broker.serve(conn)
		}
	}()
	return broker
}

// readMQTTPacket reads one control packet: its type and flags byte, and its body.
func readMQTTPacket(conn net.Conn) (byte, []byte, error) {
	var first [1]byte
	if _, err := io.ReadFull(conn, first[:]); err != nil {
		return 0, nil, err
	}
	remaining, multiplier := 0, 1
	for {
		var b [1]byte
		if _, err := io.ReadFull(conn, b[:]); err != nil {
			return 0, nil, err
		}
		remaining += int(b[0]&0x7f) * multiplier
		if b[0]&0x80 == 0 {
			break
		}
		multiplier *= 128
	}
	body := make([]byte, remaining)
	_, err := io.ReadFull(conn, body)
	return first[0], body, err
}

// mqttString reads a length-prefixed string at the start of b, and returns it and what follows.
func mqttString(b []byte) (string, []byte) {
	n := int(b[0])<<8 | int(b[1])
	return string(b[2 : 2+n]), b[2+n:]
}

func (b *fakeMQTTBroker) serve(conn net.Conn) {
	defer conn.Close()
	for {
		first, body, err := readMQTTPacket(conn)
		if err != nil {
			return
		}
		switch first >> 4 {
		case 1: // CONNECT: protocol name, level, flags, keep alive, client id, [user], [password]
			_, rest := mqttString(body)
			flags := rest[1]
			rest = rest[4:]
			b.mu.Lock()
			b.clientID, rest = mqttString(rest)
			if flags&0x80 != 0 {
				b.username, rest = mqttString(rest)
			}
			if flags&0x40 != 0 {
				b.password, _ = mqttString(rest)
			}
			b.mu.Unlock()
			_, _ = conn.Write([]byte{0x20, 0x02, 0x00, 0x00})
		case 8: // SUBSCRIBE: packet id, topic filter, requested QoS
			packetID := body[:2]
			_, _ = conn.Write([]byte{0x90, 0x03, packetID[0], packetID[1], 0x00})
			for _, message := range b.publish {
				payload := append(encodeMQTTString(message.topic), message.payload...)
				_, _ = conn.Write(append([]byte{0x30, byte(len(payload))}, payload...))
			}
		case 12: // PINGREQ
			_, _ = conn.Write([]byte{0xd0, 0x00})
		case 14: // DISCONNECT
			return
		}
	}
}

func encodeMQTTString(s string) []byte {
	return append([]byte{byte(len(s) >> 8), byte(len(s))}, s...)
}

func mqttMonitor(t *testing.T, timeout int, cfg MQTTConfig) *Monitor {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return &Monitor{ID: "mqtt-test", Name: "MQTT test", Type: "mqtt", Timeout: timeout, Config: string(raw)}
}

func TestMQTTExecutor_Execute_CheckTypes(t *testing.T) {
	const topic = "sensors/state"
	tests := []struct {
		name        string
		payload     string
		cfg         MQTTConfig
		wantStatus  shared.MonitorStatus
		wantMessage string
	}{
		{name: "none, message received", payload: "anything", cfg: MQTTConfig{CheckType: "none"}, wantStatus: shared.MonitorStatusUp, wantMessage: "Topic: sensors/state; Message received"},
		{name: "no check type counts as none", payload: "anything", cfg: MQTTConfig{}, wantStatus: shared.MonitorStatusUp, wantMessage: "Topic: sensors/state; Message received"},
		{name: "none, empty message", payload: "", cfg: MQTTConfig{CheckType: "none"}, wantStatus: shared.MonitorStatusDown, wantMessage: "Topic: sensors/state; No message received"},
		{name: "keyword found", payload: "state=ok", cfg: MQTTConfig{CheckType: "keyword", SuccessKeyword: "ok"}, wantStatus: shared.MonitorStatusUp, wantMessage: "Topic: sensors/state; Message: state=ok"},
		{name: "keyword missing", payload: "state=bad", cfg: MQTTConfig{CheckType: "keyword", SuccessKeyword: "ok"}, wantStatus: shared.MonitorStatusDown, wantMessage: "Message Mismatch - Topic: sensors/state; Message: state=bad"},
		{name: "keyword and an empty message", payload: "", cfg: MQTTConfig{CheckType: "keyword", SuccessKeyword: "ok"}, wantStatus: shared.MonitorStatusDown, wantMessage: "Message Mismatch - Topic: sensors/state; Message: "},
		{name: "blank keyword", payload: "state=ok", cfg: MQTTConfig{CheckType: "keyword", SuccessKeyword: "  "}, wantStatus: shared.MonitorStatusDown, wantMessage: "Success keyword is empty or invalid"},
		{name: "json path is blank", payload: `{"state": "ok"}`, cfg: MQTTConfig{CheckType: "json-query", JsonPath: " ", ExpectedValue: "ok"}, wantStatus: shared.MonitorStatusDown, wantMessage: "JSON path is empty or invalid"},
		{name: "expected value is blank", payload: `{"state": "ok"}`, cfg: MQTTConfig{CheckType: "json-query", JsonPath: "state", ExpectedValue: " "}, wantStatus: shared.MonitorStatusDown, wantMessage: "Expected value is empty or invalid"},
		{name: "message isn't JSON", payload: "state=ok", cfg: MQTTConfig{CheckType: "json-query", JsonPath: "state", ExpectedValue: "ok"}, wantStatus: shared.MonitorStatusDown, wantMessage: "Failed to parse JSON message: invalid character 's' looking for beginning of value"},
		{name: "expression isn't JSONata", payload: `{"state": "ok"}`, cfg: MQTTConfig{CheckType: "json-query", JsonPath: "state +", ExpectedValue: "ok"}, wantStatus: shared.MonitorStatusDown, wantMessage: "Invalid JSONata expression: "},
		{name: "expression fails", payload: `{"state": "ok"}`, cfg: MQTTConfig{CheckType: "json-query", JsonPath: `$number("abc")`, ExpectedValue: "ok"}, wantStatus: shared.MonitorStatusDown, wantMessage: "JSONata evaluation failed: "},
		{name: "json value found", payload: `{"state": "ok"}`, cfg: MQTTConfig{CheckType: "json-query", JsonPath: "state", ExpectedValue: "ok"}, wantStatus: shared.MonitorStatusUp, wantMessage: "Message received, expected value is found"},
		{name: "json number found", payload: `{"n": {"v": 3}}`, cfg: MQTTConfig{CheckType: "json-query", JsonPath: "n.v * 2", ExpectedValue: "6"}, wantStatus: shared.MonitorStatusUp, wantMessage: "Message received, expected value is found"},
		{name: "json value differs", payload: `{"state": "bad"}`, cfg: MQTTConfig{CheckType: "json-query", JsonPath: "state", ExpectedValue: "ok"}, wantStatus: shared.MonitorStatusDown, wantMessage: "Message received but value is not equal to expected value, value was: [bad]"},
		{name: "json path matches nothing", payload: `{"state": "ok"}`, cfg: MQTTConfig{CheckType: "json-query", JsonPath: "missing", ExpectedValue: "ok"}, wantStatus: shared.MonitorStatusDown, wantMessage: "JSONata evaluation failed: no results found"},
		{name: "json value is null", payload: `{"v": 1}`, cfg: MQTTConfig{CheckType: "json-query", JsonPath: "null", ExpectedValue: "x"}, wantStatus: shared.MonitorStatusDown, wantMessage: "Message received but value is not equal to expected value, value was: []"},
		{name: "unknown check type", payload: "anything", cfg: MQTTConfig{CheckType: "bogus"}, wantStatus: shared.MonitorStatusDown, wantMessage: "Unknown MQTT Check Type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			broker := startFakeMQTTBroker(t, mqttMessage{"other/topic", "ignored"}, mqttMessage{topic, tt.payload})
			tt.cfg.Hostname, tt.cfg.Port, tt.cfg.Topic = "127.0.0.1", broker.port, topic

			result := NewMQTTExecutor(zap.NewNop().Sugar()).Execute(context.Background(), mqttMonitor(t, 5, tt.cfg), nil)

			assert.Equal(t, tt.wantStatus, result.Status, result.Message)
			assert.True(t, strings.HasPrefix(result.Message, tt.wantMessage), result.Message)
			assert.False(t, result.EndTime.Before(result.StartTime))
		})
	}
}

func TestMQTTExecutor_Execute_ConnectsWithTheConfiguredCredentials(t *testing.T) {
	broker := startFakeMQTTBroker(t, mqttMessage{"t", "x"})

	result := NewMQTTExecutor(zap.NewNop().Sugar()).Execute(context.Background(), mqttMonitor(t, 5, MQTTConfig{
		Hostname: "127.0.0.1", Port: broker.port, Topic: "t", Username: "alice", Password: "s3cret",
	}), nil)

	require.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
	broker.mu.Lock()
	defer broker.mu.Unlock()
	assert.Equal(t, "alice", broker.username)
	assert.Equal(t, "s3cret", broker.password)
	assert.True(t, strings.HasPrefix(broker.clientID, "peekaping_"), broker.clientID)
	assert.Len(t, broker.clientID, len("peekaping_")+8)
}

func TestMQTTExecutor_Execute_ConnectsAnonymouslyWithoutCredentials(t *testing.T) {
	broker := startFakeMQTTBroker(t, mqttMessage{"t", "x"})

	result := NewMQTTExecutor(zap.NewNop().Sugar()).Execute(context.Background(), mqttMonitor(t, 5, MQTTConfig{
		Hostname: "127.0.0.1", Port: broker.port, Topic: "t",
	}), nil)

	require.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
	broker.mu.Lock()
	defer broker.mu.Unlock()
	assert.Empty(t, broker.username)
	assert.Empty(t, broker.password)
}

func TestMQTTExecutor_Execute_ConnectionFailures(t *testing.T) {
	t.Run("broker refuses the connection", func(t *testing.T) {
		result := NewMQTTExecutor(zap.NewNop().Sugar()).Execute(context.Background(), mqttMonitor(t, 5, MQTTConfig{
			Hostname: "127.0.0.1", Port: closedPort(t), Topic: "t",
		}), nil)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.True(t, strings.HasPrefix(result.Message, "MQTT connection failed: MQTT connection failed: "), result.Message)
	})

	t.Run("no message arrives in time", func(t *testing.T) {
		broker := startFakeMQTTBroker(t, mqttMessage{"other/topic", "ignored"})

		start := time.Now()
		result := NewMQTTExecutor(zap.NewNop().Sugar()).Execute(context.Background(), mqttMonitor(t, 1, MQTTConfig{
			Hostname: "127.0.0.1", Port: broker.port, Topic: "t",
		}), nil)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Equal(t, "MQTT connection failed: timeout, message not received within 1s", result.Message)
		assert.Less(t, time.Since(start), 3*time.Second)
	})

	t.Run("the check is cancelled", func(t *testing.T) {
		broker := startFakeMQTTBroker(t)
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(200*time.Millisecond, cancel)

		result := NewMQTTExecutor(zap.NewNop().Sugar()).Execute(ctx, mqttMonitor(t, 10, MQTTConfig{
			Hostname: "127.0.0.1", Port: broker.port, Topic: "t",
		}), nil)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Equal(t, "MQTT connection failed: context cancelled", result.Message)
	})
}
