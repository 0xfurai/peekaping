//go:build integration

package testservice

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func askForRabbitMQ(t *testing.T) (RabbitMQServer, *recorder) {
	t.Helper()
	rec := &recorder{TB: t}
	var srv RabbitMQServer
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv = RabbitMQ(rec)
	}()
	<-done
	return srv, rec
}

func TestRabbitMQ_EnvVarWins(t *testing.T) {
	dockerMustNotBeAsked(t)
	// The password is percent-encoded in the URL: p@ss:w/rd%1
	t.Setenv(rabbitMQURLEnv, "http://peekaping:p%40ss%3Aw%2Frd%251@127.0.0.1:15673")

	srv, rec := askForRabbitMQ(t)

	assert.False(t, rec.skipped || rec.failed, rec.msg)
	assert.Equal(t, RabbitMQServer{URL: "http://127.0.0.1:15673", Username: "peekaping", Password: "p@ss:w/rd%1"}, srv)
}

func TestRabbitMQ_MalformedEnvVarFails(t *testing.T) {
	dockerMustNotBeAsked(t)
	for _, raw := range []string{
		"127.0.0.1:15672",
		"http://127.0.0.1:15672",
		"http://:pw@127.0.0.1:15672",
		"http://user@127.0.0.1:15672",
		"http://user:pw@127.0.0.1",
		"amqp://user:pw@127.0.0.1:15672",
		"http://user:p%zz@127.0.0.1:15672",
	} {
		t.Setenv(rabbitMQURLEnv, raw)

		_, rec := askForRabbitMQ(t)

		assert.True(t, rec.failed, raw)
		assert.Contains(t, rec.msg, rabbitMQURLEnv, raw)
	}
}

func TestRabbitMQ_NoDocker(t *testing.T) {
	t.Setenv(rabbitMQURLEnv, "")
	stubDocker(t, func() error { return errors.New("cannot connect to the Docker daemon") })

	t.Run("skips outside CI", func(t *testing.T) {
		t.Setenv("CI", "")

		_, rec := askForRabbitMQ(t)

		require.True(t, rec.skipped, "failed=%v msg=%q", rec.failed, rec.msg)
		assert.Contains(t, rec.msg, "Docker")
		assert.Contains(t, rec.msg, rabbitMQURLEnv)
	})

	t.Run("fails in CI", func(t *testing.T) {
		t.Setenv("CI", "true")

		_, rec := askForRabbitMQ(t)

		require.True(t, rec.failed, "skipped=%v msg=%q", rec.skipped, rec.msg)
		assert.Contains(t, rec.msg, "Docker")
		assert.Contains(t, rec.msg, rabbitMQURLEnv)
	})
}
