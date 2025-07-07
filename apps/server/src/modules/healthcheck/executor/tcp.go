package executor

import (
	"context"
	"fmt"
	"net"
	"peekaping/src/modules/shared"
	"time"

	"go.uber.org/zap"
)

type TCPConfig struct {
	Host string `json:"host" validate:"required" example:"example.com"`
	Port int    `json:"port" validate:"required,min=1,max=65535" example:"80"`
}

type TCPExecutor struct {
	logger *zap.SugaredLogger
}

func NewTCPExecutor(logger *zap.SugaredLogger) *TCPExecutor {
	return &TCPExecutor{
		logger: logger,
	}
}

func (s *TCPExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[TCPConfig](configJSON)
}

func (s *TCPExecutor) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*TCPConfig))
}

func (t *TCPExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := t.Unmarshal(m.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*TCPConfig)

	t.logger.Debugf("execute tcp cfg: %+v", cfg)

	address := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	startTime := time.Now().UTC()

	// Create a custom dialer with timeout
	dialer := &net.Dialer{
		Timeout: time.Duration(m.Timeout) * time.Second,
	}

	conn, err := dialer.DialContext(ctx, "tcp", address)
	endTime := time.Now().UTC()

	if err != nil {
		t.logger.Infof("TCP connection failed: %s, %s", m.Name, err.Error())
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("TCP connection failed: %v", err),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	// Close the connection immediately as we only need to test connectivity
	conn.Close()

	t.logger.Infof("TCP connection successful: %s", m.Name)

	return &Result{
		Status:    shared.MonitorStatusUp,
		Message:   fmt.Sprintf("TCP port %d is open", cfg.Port),
		StartTime: startTime,
		EndTime:   endTime,
	}
}
