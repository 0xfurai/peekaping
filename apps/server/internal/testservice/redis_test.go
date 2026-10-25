//go:build integration

package testservice

import (
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recorder stands in for the test that asks for a service, so the tests can
// see whether it was skipped or failed. Skipf and Fatalf end the goroutine
// like the real ones do.
type recorder struct {
	testing.TB
	skipped, failed bool
	msg             string
}

func (r *recorder) Helper() {}

func (r *recorder) Skipf(format string, args ...any) {
	r.skipped, r.msg = true, fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.failed, r.msg = true, fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func askForRedis(t *testing.T) (RedisServer, *recorder) {
	t.Helper()
	rec := &recorder{TB: t}
	var srv RedisServer
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv = Redis(rec)
	}()
	<-done
	return srv, rec
}

func stubDocker(t *testing.T, check func() error) {
	t.Helper()
	orig := dockerReady
	t.Cleanup(func() { dockerReady = orig })
	dockerReady = check
}

func dockerMustNotBeAsked(t *testing.T) {
	t.Helper()
	stubDocker(t, func() error {
		t.Error("Docker was checked although the environment variable is set")
		return nil
	})
}

func TestRedis_EnvVarWins(t *testing.T) {
	dockerMustNotBeAsked(t)
	t.Setenv(redisURLEnv, "redis://:s3cret@127.0.0.1:16379")

	srv, rec := askForRedis(t)

	assert.False(t, rec.skipped || rec.failed, rec.msg)
	assert.Equal(t, RedisServer{Addr: "127.0.0.1:16379", Password: "s3cret"}, srv)
}

func TestRedis_MalformedEnvVarFails(t *testing.T) {
	dockerMustNotBeAsked(t)
	for _, raw := range []string{"127.0.0.1:6379", "redis://127.0.0.1:6379", "redis://:pw@127.0.0.1", "http://:pw@127.0.0.1:6379", "redis://user:pw@127.0.0.1:6379"} {
		t.Setenv(redisURLEnv, raw)

		_, rec := askForRedis(t)

		assert.True(t, rec.failed, raw)
		assert.Contains(t, rec.msg, redisURLEnv, raw)
	}
}

func TestRedis_NoDocker(t *testing.T) {
	t.Setenv(redisURLEnv, "")
	stubDocker(t, func() error { return errors.New("cannot connect to the Docker daemon") })

	t.Run("skips outside CI", func(t *testing.T) {
		t.Setenv("CI", "")

		_, rec := askForRedis(t)

		require.True(t, rec.skipped, "failed=%v msg=%q", rec.failed, rec.msg)
		assert.Contains(t, rec.msg, "Docker")
		assert.Contains(t, rec.msg, redisURLEnv)
	})

	t.Run("fails in CI", func(t *testing.T) {
		t.Setenv("CI", "true")

		_, rec := askForRedis(t)

		require.True(t, rec.failed, "skipped=%v msg=%q", rec.skipped, rec.msg)
		assert.Contains(t, rec.msg, "Docker")
		assert.Contains(t, rec.msg, redisURLEnv)
	})
}

func TestRecovered_PanicBecomesError(t *testing.T) {
	err := recovered(func() error { panic("rootless Docker not found") })

	assert.EqualError(t, err, "rootless Docker not found")
}
