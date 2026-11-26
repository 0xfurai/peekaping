//go:build integration

package testservice

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func askForKafka(t *testing.T) (KafkaServer, *recorder) {
	t.Helper()
	rec := &recorder{TB: t}
	var srv KafkaServer
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv = Kafka(rec)
	}()
	<-done
	return srv, rec
}

func TestKafka_EnvVarsWin(t *testing.T) {
	dockerMustNotBeAsked(t)
	t.Setenv(kafkaBrokerEnv, "127.0.0.1:19092")
	t.Setenv(kafkaSASLBrokerEnv, "127.0.0.1:19093")

	srv, rec := askForKafka(t)

	assert.False(t, rec.skipped || rec.failed, rec.msg)
	assert.Equal(t, KafkaServer{
		Broker:     "127.0.0.1:19092",
		SASLBroker: "127.0.0.1:19093",
		Username:   "peekaping",
		Password:   "peekaping-secret",
	}, srv)
}

func TestKafka_OneEnvVarFails(t *testing.T) {
	dockerMustNotBeAsked(t)
	for _, set := range []string{kafkaBrokerEnv, kafkaSASLBrokerEnv} {
		t.Run(set, func(t *testing.T) {
			t.Setenv(kafkaBrokerEnv, "")
			t.Setenv(kafkaSASLBrokerEnv, "")
			t.Setenv(set, "127.0.0.1:19092")

			_, rec := askForKafka(t)

			require.True(t, rec.failed, "skipped=%v", rec.skipped)
			assert.Contains(t, rec.msg, kafkaBrokerEnv)
			assert.Contains(t, rec.msg, kafkaSASLBrokerEnv)
		})
	}
}

func TestKafka_MalformedEnvVarFails(t *testing.T) {
	dockerMustNotBeAsked(t)
	for _, raw := range []string{"127.0.0.1", "kafka", ":9092", "127.0.0.1:"} {
		t.Setenv(kafkaBrokerEnv, raw)
		t.Setenv(kafkaSASLBrokerEnv, "127.0.0.1:19093")

		_, rec := askForKafka(t)

		assert.True(t, rec.failed, raw)
		assert.Contains(t, rec.msg, kafkaBrokerEnv, raw)
	}
}

func TestKafka_NoDocker(t *testing.T) {
	t.Setenv(kafkaBrokerEnv, "")
	t.Setenv(kafkaSASLBrokerEnv, "")
	stubDocker(t, func() error { return errors.New("cannot connect to the Docker daemon") })

	t.Run("skips outside CI", func(t *testing.T) {
		t.Setenv("CI", "")

		_, rec := askForKafka(t)

		require.True(t, rec.skipped, "failed=%v msg=%q", rec.failed, rec.msg)
		assert.Contains(t, rec.msg, "Docker")
		assert.Contains(t, rec.msg, kafkaBrokerEnv)
		assert.Contains(t, rec.msg, kafkaSASLBrokerEnv)
	})

	t.Run("fails in CI", func(t *testing.T) {
		t.Setenv("CI", "true")

		_, rec := askForKafka(t)

		require.True(t, rec.failed, "skipped=%v msg=%q", rec.skipped, rec.msg)
		assert.Contains(t, rec.msg, "Docker")
		assert.Contains(t, rec.msg, kafkaBrokerEnv)
		assert.Contains(t, rec.msg, kafkaSASLBrokerEnv)
	})
}
