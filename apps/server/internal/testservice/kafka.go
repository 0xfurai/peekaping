//go:build integration

package testservice

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	kafkaBrokerEnv     = "PEE_TEST_KAFKA_BROKER"
	kafkaSASLBrokerEnv = "PEE_TEST_KAFKA_SASL_BROKER"

	// The SASL user, with the same password for PLAIN, SCRAM-SHA-256 and
	// SCRAM-SHA-512. They are also in docker-compose.integration.yml.
	kafkaUsername = "peekaping"
	kafkaPassword = "peekaping-secret"

	kafkaImage = "apache/kafka:3.9.1"

	// The ports the broker listens on inside its container.
	kafkaPlainPort = "9092/tcp"
	kafkaSASLPort  = "9093/tcp"

	kafkaScriptPath = "/tmp/testservice-kafka-start.sh"
)

// KafkaServer is a running single-node Kafka (KRaft) with two listeners the
// clients can reach. The broker creates a missing topic when a client asks it
// to, so a test decides with the executor's allow_auto_topic_creation.
type KafkaServer struct {
	// Broker is the plaintext listener, host:port.
	Broker string
	// SASLBroker is the SASL_PLAINTEXT listener, host:port. It accepts PLAIN,
	// SCRAM-SHA-256 and SCRAM-SHA-512 for Username and Password.
	SASLBroker string
	Username   string
	Password   string
}

var sharedKafka lazy[KafkaServer]

// Kafka returns the broker for this test: the one in PEE_TEST_KAFKA_BROKER and
// PEE_TEST_KAFKA_SASL_BROKER (host:port each), or a container shared by the
// tests of the binary and removed when it exits.
func Kafka(t testing.TB) KafkaServer {
	t.Helper()
	return resolve(t, "Kafka", []string{kafkaBrokerEnv, kafkaSASLBrokerEnv}, "host:port for each", parseKafkaBrokers, &sharedKafka, startKafka)
}

func parseKafkaBrokers(values []string) (KafkaServer, error) {
	for i, name := range []string{kafkaBrokerEnv, kafkaSASLBrokerEnv} {
		host, port, err := net.SplitHostPort(values[i])
		if err != nil {
			return KafkaServer{}, fmt.Errorf("%s: %w", name, err)
		}
		if host == "" || port == "" {
			return KafkaServer{}, fmt.Errorf("%s: need a host and a port", name)
		}
	}
	return KafkaServer{Broker: values[0], SASLBroker: values[1], Username: kafkaUsername, Password: kafkaPassword}, nil
}

// kafkaStartScript configures the broker and starts it, once the container
// knows the host ports Docker mapped: the broker has to advertise them, since
// that is where a client reconnects after the first metadata response. The
// format step creates the SCRAM users in the cluster metadata, and the JAAS
// config of the SASL listener holds the PLAIN user. The compose file runs the
// same script with fixed ports.
//
// The arguments are the advertised host, the plaintext port and the SASL port.
const kafkaStartScript = `#!/bin/bash
set -e
ADV_HOST=%s
ADV_PLAIN_PORT=%s
ADV_SASL_PORT=%s
cat > /tmp/server.properties <<EOF
process.roles=broker,controller
node.id=1
controller.quorum.voters=1@localhost:9095
listeners=PLAINTEXT://:9092,SASL_PLAINTEXT://:9093,BROKER://localhost:9094,CONTROLLER://localhost:9095
advertised.listeners=PLAINTEXT://${ADV_HOST}:${ADV_PLAIN_PORT},SASL_PLAINTEXT://${ADV_HOST}:${ADV_SASL_PORT},BROKER://localhost:9094
listener.security.protocol.map=PLAINTEXT:PLAINTEXT,SASL_PLAINTEXT:SASL_PLAINTEXT,BROKER:PLAINTEXT,CONTROLLER:PLAINTEXT
inter.broker.listener.name=BROKER
controller.listener.names=CONTROLLER
sasl.enabled.mechanisms=PLAIN,SCRAM-SHA-256,SCRAM-SHA-512
listener.name.sasl_plaintext.plain.sasl.jaas.config=org.apache.kafka.common.security.plain.PlainLoginModule required user_peekaping="peekaping-secret";
listener.name.sasl_plaintext.scram-sha-256.sasl.jaas.config=org.apache.kafka.common.security.scram.ScramLoginModule required;
listener.name.sasl_plaintext.scram-sha-512.sasl.jaas.config=org.apache.kafka.common.security.scram.ScramLoginModule required;
auto.create.topics.enable=true
num.partitions=1
offsets.topic.replication.factor=1
transaction.state.log.replication.factor=1
transaction.state.log.min.isr=1
group.initial.rebalance.delay.ms=0
log.dirs=/tmp/kraft-logs
EOF
/opt/kafka/bin/kafka-storage.sh format --ignore-formatted \
  -t "$(/opt/kafka/bin/kafka-storage.sh random-uuid)" -c /tmp/server.properties \
  --add-scram 'SCRAM-SHA-256=[name=peekaping,password=peekaping-secret]' \
  --add-scram 'SCRAM-SHA-512=[name=peekaping,password=peekaping-secret]'
exec /opt/kafka/bin/kafka-server-start.sh /tmp/server.properties
`

func startKafka(ctx context.Context) (KafkaServer, error) {
	// The container's command waits for the script, which the hook below copies
	// in once the ports are mapped (the pattern of testcontainers' Kafka module).
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        kafkaImage,
			ExposedPorts: []string{kafkaPlainPort, kafkaSASLPort},
			Entrypoint:   []string{"bash", "-c"},
			Cmd:          []string{"while [ ! -f " + kafkaScriptPath + " ]; do sleep 0.1; done; exec bash " + kafkaScriptPath},
			LifecycleHooks: []testcontainers.ContainerLifecycleHooks{{
				PostStarts: []testcontainers.ContainerHook{
					func(ctx context.Context, c testcontainers.Container) error {
						host, plain, sasl, err := kafkaEndpoints(ctx, c)
						if err != nil {
							return err
						}
						script := fmt.Sprintf(kafkaStartScript, host, plain, sasl)
						if err := c.CopyToContainer(ctx, []byte(script), kafkaScriptPath, 0o755); err != nil {
							return fmt.Errorf("copy the start script: %w", err)
						}
						return wait.ForLog("Kafka Server started").WaitUntilReady(ctx, c)
					},
				},
			}},
		},
		Started: true,
	})
	if err != nil {
		return KafkaServer{}, err
	}

	host, plain, sasl, err := kafkaEndpoints(ctx, c)
	if err != nil {
		return KafkaServer{}, err
	}
	srv := KafkaServer{
		Broker:     net.JoinHostPort(host, plain),
		SASLBroker: net.JoinHostPort(host, sasl),
		Username:   kafkaUsername,
		Password:   kafkaPassword,
	}
	if err := waitForKafka(ctx, srv); err != nil {
		return KafkaServer{}, err
	}
	return srv, nil
}

func kafkaEndpoints(ctx context.Context, c testcontainers.Container) (host, plain, sasl string, err error) {
	if host, err = c.Host(ctx); err != nil {
		return "", "", "", err
	}
	p, err := c.MappedPort(ctx, kafkaPlainPort)
	if err != nil {
		return "", "", "", err
	}
	s, err := c.MappedPort(ctx, kafkaSASLPort)
	if err != nil {
		return "", "", "", err
	}
	return host, p.Port(), s.Port(), nil
}

// waitForKafka returns once a client gets cluster metadata from both
// listeners, signing in to the SASL one. That is what the tests do first, and
// it shows the advertised addresses work from the host.
func waitForKafka(ctx context.Context, srv KafkaServer) error {
	plain := sarama.NewConfig()
	plain.Net.DialTimeout = 2 * time.Second

	sasl := sarama.NewConfig()
	sasl.Net.DialTimeout = 2 * time.Second
	sasl.Net.SASL.Enable = true
	sasl.Net.SASL.Mechanism = sarama.SASLTypePlaintext
	sasl.Net.SASL.User = srv.Username
	sasl.Net.SASL.Password = srv.Password

	var lastErr error
	for ctx.Err() == nil {
		lastErr = errors.Join(probeKafka(srv.Broker, plain), probeKafka(srv.SASLBroker, sasl))
		if lastErr == nil {
			return nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("no answer from Kafka: %w", lastErr)
}

func probeKafka(addr string, cfg *sarama.Config) error {
	client, err := sarama.NewClient([]string{addr}, cfg)
	if err != nil {
		return fmt.Errorf("%s: %w", addr, err)
	}
	return client.Close()
}

