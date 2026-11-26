//go:build integration && !slim

package executor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"peekaping/internal/modules/shared"
	"peekaping/internal/testservice"

	"github.com/IBM/sarama"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestKafkaProducerExecutor_Integration(t *testing.T) {
	srv := testservice.Kafka(t)
	exec := NewKafkaProducerExecutor(zap.NewNop().Sugar())

	// Topic names carry the run's time, so a broker that outlives the run
	// (the compose one) never holds a message from an earlier one.
	runID := time.Now().UnixNano()
	topicName := func(name string) string { return fmt.Sprintf("it-%s-%d", name, runID) }

	run := func(t *testing.T, cfg KafkaProducerConfig) *Result {
		t.Helper()
		return exec.Execute(context.Background(), kafkaMonitor(t, 10, cfg), nil)
	}
	plain := func(topic string) KafkaProducerConfig {
		return KafkaProducerConfig{
			Brokers:     []string{srv.Broker},
			Topic:       topic,
			Message:     "peekaping integration test " + topic,
			SASLOptions: KafkaProducerSASLOptions{Mechanism: "None"},
		}
	}
	sasl := func(topic, mechanism, password string) KafkaProducerConfig {
		cfg := plain(topic)
		cfg.Brokers = []string{srv.SASLBroker}
		cfg.SASLOptions = KafkaProducerSASLOptions{Mechanism: mechanism, Username: srv.Username, Password: password}
		return cfg
	}

	admin, err := sarama.NewClusterAdmin([]string{srv.Broker}, sarama.NewConfig())
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })
	createTopic := func(t *testing.T, topic string) {
		t.Helper()
		require.NoError(t, admin.CreateTopic(topic, &sarama.TopicDetail{NumPartitions: 1, ReplicationFactor: 1}, false))
	}
	topicExists := func(t *testing.T, topic string) bool {
		t.Helper()
		topics, err := admin.ListTopics()
		require.NoError(t, err)
		_, ok := topics[topic]
		return ok
	}

	t.Run("is up and delivers the message to an existing topic", func(t *testing.T) {
		topic := topicName("existing")
		createTopic(t, topic)
		cfg := plain(topic)

		result := run(t, cfg)

		assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		assert.Contains(t, result.Message, "Message sent successfully to topic '"+topic+"'")
		assert.Equal(t, []string{cfg.Message}, readMessages(t, srv.Broker, topic, 1))
	})

	t.Run("creates a missing topic when auto creation is on", func(t *testing.T) {
		topic := topicName("autocreate")
		cfg := plain(topic)
		cfg.AllowAutoTopicCreation = true

		result := run(t, cfg)

		assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		assert.True(t, topicExists(t, topic), "the topic was not created")
	})

	t.Run("is down for a missing topic when auto creation is off", func(t *testing.T) {
		topic := topicName("missing")
		cfg := plain(topic)
		cfg.AllowAutoTopicCreation = false

		result := run(t, cfg)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "Failed to send message")
		assert.Contains(t, result.Message, "kafka server: Request was for a topic or partition that does not exist on this broker")
		assert.False(t, topicExists(t, topic), "the topic was created")
	})

	for _, mechanism := range []string{"PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"} {
		t.Run("is up with "+mechanism+" and the right password", func(t *testing.T) {
			topic := topicName("sasl-" + mechanism)
			createTopic(t, topic)

			result := run(t, sasl(topic, mechanism, srv.Password))

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
			assert.Equal(t, []string{"peekaping integration test " + topic}, readMessages(t, srv.Broker, topic, 1))
		})

		t.Run("is down with "+mechanism+" and a wrong password", func(t *testing.T) {
			topic := topicName("sasl-wrong-" + mechanism)
			createTopic(t, topic)

			result := run(t, sasl(topic, mechanism, "not-"+srv.Password))

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Contains(t, result.Message, "Failed to create Kafka producer")
			assert.Contains(t, result.Message, "kafka server: SASL Authentication failed")
		})
	}

	t.Run("is down when SSL is on and the listener is plaintext", func(t *testing.T) {
		topic := topicName("ssl")
		createTopic(t, topic)
		cfg := plain(topic)
		cfg.SSL = true

		result := run(t, cfg)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "Failed to create Kafka producer")
		assert.Contains(t, result.Message, "kafka: client has run out of available brokers to talk to: EOF") // Kafka closes a connection that starts with a TLS handshake, so no TLS error comes back
	})
}

// readMessages reads the first n messages of the topic's only partition.
func readMessages(t *testing.T, broker, topic string, n int) []string {
	t.Helper()
	consumer, err := sarama.NewConsumer([]string{broker}, sarama.NewConfig())
	require.NoError(t, err)
	defer consumer.Close()
	partition, err := consumer.ConsumePartition(topic, 0, sarama.OffsetOldest)
	require.NoError(t, err)
	defer partition.Close()

	var got []string
	deadline := time.After(10 * time.Second)
	for len(got) < n {
		select {
		case msg := <-partition.Messages():
			got = append(got, string(msg.Value))
		case <-deadline:
			require.FailNowf(t, "timed out reading the topic", "read %d of %d messages from %s", len(got), n, topic)
		}
	}
	return got
}
