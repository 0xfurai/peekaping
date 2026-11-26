package probe_notification

import (
	"context"

	"peekaping/internal/modules/workspace"

	"go.uber.org/zap"
)

// Service keeps the notification channels of each probe: the channels a probe's own events go to
type Service interface {
	FindByProbeID(ctx context.Context, probeID string) ([]*Model, error)
	// Replace makes notificationIDs the probe's channels. Each must belong to one of the workspaces,
	// the ones the caller is a member of: a channel of another workspace is refused as one that
	// doesn't exist (ErrUnknownChannel).
	Replace(ctx context.Context, probeID string, notificationIDs []string, workspaces []workspace.ID) error
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

func (s *ServiceImpl) Replace(ctx context.Context, probeID string, notificationIDs []string, workspaces []workspace.ID) error {
	return s.repository.Replace(ctx, probeID, notificationIDs, workspaces)
}

func (s *ServiceImpl) DeleteByNotificationID(ctx context.Context, notificationID string) error {
	return s.repository.DeleteByNotificationID(ctx, notificationID)
}
