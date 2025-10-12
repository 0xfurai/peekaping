package executor

import (
	"context"

	"go.uber.org/zap"
)

type PushConfig struct {
	PushToken string `json:"pushToken" validate:"required"`
}

type PushExecutor struct {
	logger *zap.SugaredLogger
}

func NewPushExecutor(logger *zap.SugaredLogger) *PushExecutor {
	return &PushExecutor{
		logger: logger,
	}
}

func (s *PushExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[PushConfig](configJSON)
}

func (s *PushExecutor) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*PushConfig))
}

func (s *PushExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	// Push monitors are passive - they don't actively check anything.
	// The status is determined by whether the push endpoint receives calls in time.
	// Status determination based on heartbeat age should be handled by a separate
	// stateful service that monitors heartbeat timestamps.
	// When the executor is called, it just indicates the monitor configuration is valid.
	// var startTime, endTime = time.Now().UTC(), time.Now().UTC()

	// s.logger.Debugf("Push executor called for monitor %s - returning nil (no-op)", m.ID)

	// Return nil to indicate no active check needed (push monitors are passive)
	// The actual status is determined when the push endpoint is called

	return nil
}
