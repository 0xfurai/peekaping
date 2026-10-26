//go:build integration

package testservice

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	redisURLEnv = "PEE_TEST_REDIS_URL"

	// redisPassword is also in docker-compose.integration.yml. Redis has to
	// require one, so tests can check a wrong password.
	redisPassword = "integration-test-password"
)

// RedisServer is a running Redis that requires a password.
type RedisServer struct {
	Addr     string // host:port
	Password string
}

var sharedRedis lazy[RedisServer]

// Redis returns the Redis for this test: the server in PEE_TEST_REDIS_URL
// (redis://:<password>@<host>:<port>), or a container shared by the tests of
// the binary and removed when it exits.
func Redis(t testing.TB) RedisServer {
	t.Helper()
	return resolve(t, "Redis", []string{redisURLEnv}, "redis://:<password>@host:port",
		func(values []string) (RedisServer, error) { return parseRedisURL(values[0]) }, &sharedRedis, startRedis)
}

func parseRedisURL(raw string) (RedisServer, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return RedisServer{}, err
	}
	password, _ := u.User.Password()
	if u.Scheme != "redis" || u.Host == "" || u.Port() == "" || password == "" {
		return RedisServer{}, errors.New("need scheme redis, a host, a port and a password")
	}
	// The tests authenticate as the default user, so a named ACL user would be
	// dropped and its password checked against the wrong account.
	if u.User.Username() != "" {
		return RedisServer{}, errors.New("no username: the tests authenticate as the default user")
	}
	return RedisServer{Addr: u.Host, Password: password}, nil
}

func startRedis(ctx context.Context) (RedisServer, error) {
	// Nothing to persist, and a failed save must not make Redis refuse writes.
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			Cmd:          []string{"redis-server", "--requirepass", redisPassword, "--save", "", "--appendonly", "no"},
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor: wait.ForAll(
				wait.ForLog("Ready to accept connections"),
				wait.ForListeningPort("6379/tcp"),
			),
		},
		Started: true,
	})
	if err != nil {
		return RedisServer{}, err
	}

	host, err := c.Host(ctx)
	if err != nil {
		return RedisServer{}, err
	}
	port, err := c.MappedPort(ctx, "6379/tcp")
	if err != nil {
		return RedisServer{}, err
	}
	return RedisServer{Addr: net.JoinHostPort(host, port.Port()), Password: redisPassword}, nil
}
