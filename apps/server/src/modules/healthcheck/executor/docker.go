package executor

import (
	"context"
	"fmt"
	"peekaping/src/modules/shared"
	"time"

	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

type DockerConfig struct {
	ContainerID    string `json:"container_id" validate:"required"`
	ConnectionType string `json:"connection_type" validate:"required,oneof=socket tcp"`
	DockerDaemon   string `json:"docker_daemon" validate:"required"`
}

type DockerExecutor struct {
	logger *zap.SugaredLogger
}

func NewDockerExecutor(logger *zap.SugaredLogger) *DockerExecutor {
	return &DockerExecutor{logger: logger}
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

func (e *DockerExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	start := time.Now().UTC()
	cfgAny, err := e.Unmarshal(m.Config)
	if err != nil {
		return DownResult(fmt.Errorf("invalid config: %w", err), start, time.Now().UTC())
	}
	cfg := cfgAny.(*DockerConfig)

	e.logger.Debugf("execute docker cfg: %+v", cfg)

	var cli *client.Client
	var cliErr error

	if cfg.ConnectionType == "socket" {
		cli, cliErr = client.NewClientWithOpts(
			client.WithHost("unix://"+cfg.DockerDaemon),
			client.WithAPIVersionNegotiation(),
		)
	} else if cfg.ConnectionType == "tcp" {
		cli, cliErr = client.NewClientWithOpts(
			client.WithHost(cfg.DockerDaemon),
			client.WithAPIVersionNegotiation(),
		)
		// TLS support can be added here in the future
	} else {
		return DownResult(fmt.Errorf("unknown docker connection type: %s", cfg.ConnectionType), start, time.Now().UTC())
	}

	if cliErr != nil {
		return DownResult(fmt.Errorf("docker client error: %w", cliErr), start, time.Now().UTC())
	}
	defer cli.Close()

	container, err := cli.ContainerInspect(ctx, cfg.ContainerID)
	if err != nil {
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
