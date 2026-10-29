//go:build !slim

package executor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"peekaping/internal/modules/shared"
	"strings"
	"time"

	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

type DockerConfig struct {
	ContainerID    string `json:"container_id" validate:"required"`
	ConnectionType string `json:"connection_type" validate:"required,oneof=socket tcp"`
	DockerDaemon   string `json:"docker_daemon" validate:"required"`
	// TLS fields
	TLSEnabled bool   `json:"tls_enabled,omitempty"`
	TLSCert    string `json:"tls_cert,omitempty"`
	TLSKey     string `json:"tls_key,omitempty"`
	TLSCA      string `json:"tls_ca,omitempty"`
	TLSVerify  bool   `json:"tls_verify,omitempty"`
}

type DockerExecutor struct {
	guarded
	logger *zap.SugaredLogger
}

func NewDockerExecutor(logger *zap.SugaredLogger) *DockerExecutor {
	return &DockerExecutor{logger: logger}
}

func init() { registerOptional("docker", NewDockerExecutor) }

// withoutEnvironmentProxy has the Docker client connect to the daemon without the proxy that
// HTTP_PROXY and HTTPS_PROXY name. The client's HTTPClient is a copy, but of a client that holds
// the same transport.
func withoutEnvironmentProxy() client.Opt {
	return func(c *client.Client) error {
		if transport, ok := c.HTTPClient().Transport.(*http.Transport); ok {
			transport.Proxy = nil
			return nil
		}
		return fmt.Errorf("cannot turn the proxy off on transport: %T", c.HTTPClient().Transport)
	}
}

func (e *DockerExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[DockerConfig](configJSON)
}

func (e *DockerExecutor) Validate(configJSON string) error {
	cfg, err := e.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*DockerConfig))
}

// Targets lists the host of the Docker daemon when the check connects to it over TCP. A socket has
// no host: the guard refuses it when the check connects.
func (e *DockerExecutor) Targets(configJSON string) ([]string, error) {
	cfg, err := GenericUnmarshal[DockerConfig](configJSON)
	if err != nil {
		return nil, err
	}
	if cfg.ConnectionType != "tcp" {
		return nil, nil
	}
	u, err := client.ParseHostURL(cfg.DockerDaemon)
	if err != nil {
		return []string{hostOf(cfg.DockerDaemon)}, nil
	}
	switch u.Scheme {
	case "unix", "npipe":
		return nil, nil
	}
	return []string{hostOf(u.Host)}, nil
}

// socketRefusal is the guard's refusal of a daemon that is reached over a Unix socket or a named
// pipe, which is a socket for the guard's purposes: the daemon of a "socket" connection, or one
// that a "tcp" connection names with unix:// or npipe://. The dialer refuses them as well, but
// the Docker client hands it the socket's path as if it were a TCP address, which fails with a
// less telling error.
func (e *DockerExecutor) socketRefusal(cfg *DockerConfig) error {
	network, address := "unix", cfg.DockerDaemon
	if cfg.ConnectionType == "tcp" {
		u, err := client.ParseHostURL(cfg.DockerDaemon)
		if err != nil || (u.Scheme != "unix" && u.Scheme != "npipe") {
			return nil
		}
		network, address = u.Scheme, u.Host
	}
	return e.guard.Control(network, address, nil)
}

func (e *DockerExecutor) createTLSConfig(cfg *DockerConfig) (*tls.Config, error) {
	if !cfg.TLSEnabled {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: !cfg.TLSVerify,
	}

	// Load client certificate and key if provided
	if cfg.TLSCert != "" || cfg.TLSKey != "" {
		// Both certificate and key must be provided together
		if cfg.TLSCert == "" || cfg.TLSKey == "" {
			return nil, fmt.Errorf("both certificate and key must be provided for client authentication")
		}

		cert, err := tls.X509KeyPair([]byte(cfg.TLSCert), []byte(cfg.TLSKey))
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// Load CA certificate if provided
	if cfg.TLSCA != "" {
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM([]byte(cfg.TLSCA)) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
	}

	return tlsConfig, nil
}

func (e *DockerExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	start := time.Now().UTC()
	cfgAny, err := e.Unmarshal(m.Config)
	if err != nil {
		return DownResult(fmt.Errorf("invalid config: %w", err), start, time.Now().UTC())
	}
	cfg := cfgAny.(*DockerConfig)

	if e.on() {
		if err := e.socketRefusal(cfg); err != nil {
			return DownResult(err, start, time.Now().UTC())
		}
	}

	var cli *client.Client
	var cliErr error

	if cfg.ConnectionType == "socket" {
		cli, cliErr = client.NewClientWithOpts(
			client.WithHost("unix://"+cfg.DockerDaemon),
			client.WithAPIVersionNegotiation(),
		)
	} else if cfg.ConnectionType == "tcp" {
		clientOpts := []client.Opt{
			client.WithHost(cfg.DockerDaemon),
			client.WithAPIVersionNegotiation(),
		}

		// Add TLS support for TCP connections
		if cfg.TLSEnabled {
			tlsConfig, err := e.createTLSConfig(cfg)
			if err != nil {
				return DownResult(fmt.Errorf("TLS configuration error: %w", err), start, time.Now().UTC())
			}

			httpClient := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: tlsConfig,
				},
			}
			clientOpts = append(clientOpts, client.WithHTTPClient(httpClient))
		}

		// Dial through the guard while it is on. This comes after the HTTP client above, so it is
		// the dial function of that client's transport that it replaces. The daemon is dialed
		// itself: through a proxy from the environment, which WithHost sets up for a tcp://
		// daemon, the guard would check the proxy's address and not the daemon's.
		if e.on() {
			clientOpts = append(clientOpts,
				client.WithDialContext(e.dialer(&net.Dialer{Timeout: 32 * time.Second}).DialContext),
				withoutEnvironmentProxy(),
			)
		}

		cli, cliErr = client.NewClientWithOpts(clientOpts...)
	} else {
		return DownResult(fmt.Errorf("unknown docker connection type: %s", cfg.ConnectionType), start, time.Now().UTC())
	}

	if cliErr != nil {
		return DownResult(fmt.Errorf("docker client error: %w", cliErr), start, time.Now().UTC())
	}
	defer cli.Close()

	container, err := cli.ContainerInspect(ctx, cfg.ContainerID)
	if err != nil {
		// Provide better error messages for common TLS issues
		errorMsg := err.Error()
		if strings.Contains(errorMsg, "certificate relies on legacy Common Name field") {
			return DownResult(fmt.Errorf("container inspect error: %w\n\nHint: The Docker daemon's TLS certificate uses legacy Common Name instead of Subject Alternative Names. Try setting 'Verify TLS' to false, or update your Docker daemon with a proper certificate that includes SANs", err), start, time.Now().UTC())
		}
		if strings.Contains(errorMsg, "x509: certificate signed by unknown authority") {
			return DownResult(fmt.Errorf("container inspect error: %w\n\nHint: The Docker daemon's certificate is not trusted. Provide a CA certificate or set 'Verify TLS' to false", err), start, time.Now().UTC())
		}
		if strings.Contains(errorMsg, "tls: failed to verify certificate") {
			return DownResult(fmt.Errorf("container inspect error: %w\n\nHint: TLS certificate verification failed. Check your certificates or set 'Verify TLS' to false for testing", err), start, time.Now().UTC())
		}
		return DownResult(fmt.Errorf("container inspect error: %w", err), start, time.Now().UTC())
	}

	endTime := time.Now().UTC()

	if container.State != nil && container.State.Running {
		if container.State.Health != nil && container.State.Health.Status != "healthy" {
			// Handle different health statuses appropriately
			switch container.State.Health.Status {
			case "starting":
				return &Result{
					Status:    shared.MonitorStatusPending,
					Message:   container.State.Health.Status,
					StartTime: start,
					EndTime:   endTime,
				}
			case "unhealthy":
				return DownResult(fmt.Errorf("container is unhealthy: %s", container.State.Health.Status), start, endTime)
			default:
				// For any other non-healthy status, consider it down
				return DownResult(fmt.Errorf("container health status: %s", container.State.Health.Status), start, endTime)
			}
		}
		var message string
		if container.State.Health != nil {
			message = container.State.Health.Status
		} else {
			message = container.State.Status
		}
		return &Result{
			Status:    shared.MonitorStatusUp,
			Message:   message,
			StartTime: start,
			EndTime:   endTime,
		}
	}

	if container.State == nil {
		return DownResult(fmt.Errorf("container state is nil"), start, endTime)
	}

	return DownResult(fmt.Errorf("container state is %s", container.State.Status), start, endTime)
}
