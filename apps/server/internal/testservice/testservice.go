//go:build integration

// Package testservice gives integration tests a real server to talk to: Redis,
// Kafka, PostgreSQL, MySQL, MongoDB (standalone and a replica set) and SQL
// Server now, and later the other services the monitor executors check. It is
// built only under the integration build tag, so a plain "go test" never
// compiles testcontainers or needs Docker.
//
// Each service has one function, such as Redis. It returns the address of the
// server named by the service's environment variable (Kafka has two), or, when
// that is unset, of a container it starts. The compose file in apps/server runs
// the same services for CI.
package testservice

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
)

// startTimeout bounds the Docker check and the container start. A first run
// may have to pull the image.
const startTimeout = 3 * time.Minute

// lazy starts a service once per test binary and remembers a failure too, so
// a broken Docker setup costs one timeout, not one per test.
type lazy[T any] struct {
	once sync.Once
	val  T
	err  error
}

func (l *lazy[T]) get(start func(context.Context) (T, error)) (T, error) {
	l.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
		defer cancel()
		l.val, l.err = start(ctx)
	})
	return l.val, l.err
}

// resolve is what every service function builds on. With the service's
// environment variables set it parses their values and starts nothing, and it
// doesn't check that anything listens: the test fails on its own when nothing
// does. A service with several variables needs all of them, or none. Without
// them, resolve starts the service in Docker. When Docker is missing the test
// skips, except under CI, where a skip would let a broken job go green.
func resolve[T any](
	t testing.TB,
	service string, envVars []string, example string,
	parse func(values []string) (T, error),
	shared *lazy[T],
	start func(context.Context) (T, error),
) T {
	t.Helper()

	names := strings.Join(envVars, " and ")
	values := make([]string, len(envVars))
	set := 0
	for i, name := range envVars {
		if values[i] = os.Getenv(name); values[i] != "" {
			set++
		}
	}
	if set > 0 {
		if set < len(envVars) {
			t.Fatalf("%s is not usable: set all of them or none, but only some are set (expected %s)", names, example)
		}
		v, err := parse(values)
		if err != nil {
			t.Fatalf("%s is not usable: %v (expected %s)", names, err, example)
		}
		return v
	}

	if err := dockerReady(); err != nil {
		msg := "%s integration test needs Docker (%v): start Docker, or set %s to a running %s (%s)"
		if os.Getenv("CI") != "" {
			t.Fatalf(msg, service, err, names, service, example)
		}
		t.Skipf(msg, service, err, names, service, example)
	}

	v, err := shared.get(start)
	if err != nil {
		t.Fatalf("start %s container: %v", service, err)
	}
	return v
}

// dockerReady reports whether Docker answers. A test replaces it.
var dockerReady = sync.OnceValue(func() error { return recovered(pingDocker) })

func pingDocker() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return err
	}
	return provider.Health(ctx) // closes the provider
}

// recovered turns a panic into an error. The testcontainers client panics,
// instead of returning one, when it finds no Docker host, which is the case
// the Docker check exists for.
func recovered(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return fn()
}
