package probe_notification

import (
	"context"

	"go.uber.org/zap"
)

// Service keeps the notification channels of each probe: the channels a probe's own events go to
type Service interface {
	FindByProbeID(ctx context.Context, probeID string) ([]*Model, error)
	Replace(ctx context.Context, probeID string, notificationIDs []string) error
	DeleteByNotificationID(ctx context.Context, notificationID string) error
}

type ServiceImpl struct {
	repository Repository
	logger     *zap.SugaredLogger
}

func NewService(
	repository Repository,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repository,
		logger.Named("[probe-notification-service]"),
	}
}

func (s *ServiceImpl) FindByProbeID(ctx context.Context, probeID string) ([]*Model, error) {
	return s.repository.FindByProbeID(ctx, probeID)
}

func (s *ServiceImpl) Replace(ctx context.Context, probeID string, notificationIDs []string) error {
	return s.repository.Replace(ctx, probeID, notificationIDs)
}

func (s *ServiceImpl) DeleteByNotificationID(ctx context.Context, notificationID string) error {
	return s.repository.DeleteByNotificationID(ctx, notificationID)
}
