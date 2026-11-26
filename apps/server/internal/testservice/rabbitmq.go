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
	rabbitMQURLEnv = "PEE_TEST_RABBITMQ_URL"

	// The broker's only user, also in docker-compose.integration.yml. The
	// password has characters a URL reserves (@ : / %), so a test can check
	// that the executor sends it as it is. In rabbitMQURLEnv it is
	// percent-encoded: p%40ss%3Aw%2Frd%251.
	rabbitMQUsername = "peekaping"
	rabbitMQPassword = "p@ss:w/rd%1"

	rabbitMQImage = "rabbitmq:4-management-alpine"

	// The port of the management API inside the container.
	rabbitMQManagementPort = "15672/tcp"
)

// RabbitMQServer is a running RabbitMQ with its management API enabled. The
// executor talks to that API over HTTP and never opens an AMQP connection, so
// that is all the server offers.
type RabbitMQServer struct {
	// URL is the API's base URL, http://host:port, without credentials.
	URL string
	// Username and Password are the broker's only user. The password holds
	// reserved URL characters.
	Username string
	Password string
}

var sharedRabbitMQ lazy[RabbitMQServer]

// RabbitMQ returns the broker for this test: the one in PEE_TEST_RABBITMQ_URL
// (http://<user>:<password>@<host>:<port>, the password percent-encoded), or a
// container shared by the tests of the binary and removed when it exits.
func RabbitMQ(t testing.TB) RabbitMQServer {
	t.Helper()
	return resolve(t, "RabbitMQ", []string{rabbitMQURLEnv}, "http://<user>:<password>@host:port, the password percent-encoded",
		func(values []string) (RabbitMQServer, error) { return parseRabbitMQURL(values[0]) }, &sharedRabbitMQ, startRabbitMQ)
}

func parseRabbitMQURL(raw string) (RabbitMQServer, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return RabbitMQServer{}, err
	}
	username := u.User.Username()
	password, _ := u.User.Password()
	if u.Scheme != "http" || u.Host == "" || u.Port() == "" || username == "" || password == "" {
		return RabbitMQServer{}, errors.New("need scheme http, a host, a port, a username and a password")
	}
	return RabbitMQServer{URL: (&url.URL{Scheme: u.Scheme, Host: u.Host}).String(), Username: username, Password: password}, nil
}

func startRabbitMQ(ctx context.Context) (RabbitMQServer, error) {
	// The wait asks the alarms endpoint, the one the executor checks, with the
	// credentials the tests use. It answers 200 once the management plugin is up
	// and the user exists.
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: rabbitMQImage,
			Env: map[string]string{
				"RABBITMQ_DEFAULT_USER": rabbitMQUsername,
				"RABBITMQ_DEFAULT_PASS": rabbitMQPassword,
			},
			ExposedPorts: []string{rabbitMQManagementPort},
			WaitingFor: wait.ForHTTP("/api/health/checks/alarms/").
				WithPort(rabbitMQManagementPort).
				WithBasicAuth(rabbitMQUsername, rabbitMQPassword).
				WithStatusCodeMatcher(func(status int) bool { return status == 200 }),
		},
		Started: true,
	})
	if err != nil {
		return RabbitMQServer{}, err
	}

	host, err := c.Host(ctx)
	if err != nil {
		return RabbitMQServer{}, err
	}
	port, err := c.MappedPort(ctx, rabbitMQManagementPort)
	if err != nil {
		return RabbitMQServer{}, err
	}
	return RabbitMQServer{
		URL:      "http://" + net.JoinHostPort(host, port.Port()),
		Username: rabbitMQUsername,
		Password: rabbitMQPassword,
	}, nil
}
