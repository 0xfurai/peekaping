package executor

import (
	"context"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/shared"
	"time"

	"go.uber.org/zap"
)

type PushConfig struct {
	PushToken string `json:"pushToken" validate:"required"`
}

type PushExecutor struct {
	logger           *zap.SugaredLogger
	heartbeatService heartbeat.Service
}

func NewPushExecutor(logger *zap.SugaredLogger, heartbeatService heartbeat.Service) *PushExecutor {
	return &PushExecutor{
		logger:           logger,
		heartbeatService: heartbeatService,
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
	latestHeartbeats, err := s.heartbeatService.FindByMonitorIDPaginated(ctx, m.ID, 1, 0, nil, false)

	if err != nil {
		s.logger.Errorf("Failed to fetch latest heartbeat for monitor %s: %v", m.ID, err)
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   "Failed to fetch heartbeat: " + err.Error(),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	var status shared.MonitorStatus
	var message string

	if len(latestHeartbeats) > 0 {
		hb := latestHeartbeats[0]
		s.logger.Infof("Latest heartbeat: %v", hb)
		timeSince := time.Since(hb.Time)
		s.logger.Infof("Time since last heartbeat: %v", timeSince)
		if timeSince <= time.Duration(m.Interval)*time.Second {
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
