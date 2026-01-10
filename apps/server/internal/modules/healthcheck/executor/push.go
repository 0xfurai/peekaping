package executor

import (
	"context"
	"vigi/internal/modules/shared"
	"time"

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
	// Check for the latest heartbeat for this monitor
	var startTime, endTime = time.Now().UTC(), time.Now().UTC()

	var status shared.MonitorStatus
	var message string

	if m.LastHeartbeat != nil {
		s.logger.Infof("Latest heartbeat: %v", m.LastHeartbeat)
		timeSince := time.Since(m.LastHeartbeat.Time)
		s.logger.Infof("Time since last heartbeat: %v", timeSince)

		if m.LastHeartbeat.Status == 1 && timeSince <= time.Duration(m.Interval)*time.Second {
			s.logger.Infof("Push received in time")
			return nil
		} else {
			s.logger.Infof("Push received too late")
			status = shared.MonitorStatusDown
			message = "No push received in time"
		}
	} else {
		s.logger.Infof("No heartbeat found")
		status = shared.MonitorStatusDown
		message = "No push received yet"
	}

	return &Result{
		Status:    status,
		Message:   message,
		StartTime: startTime,
		EndTime:   endTime,
	}
}
