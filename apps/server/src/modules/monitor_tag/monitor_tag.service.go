package monitor_tag

import (
	"context"

	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, monitorID string, tagID string) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	Delete(ctx context.Context, id string) error
	FindByMonitorID(ctx context.Context, monitorID string) ([]*Model, error)
	FindByTagID(ctx context.Context, tagID string) ([]*Model, error)
	DeleteByMonitorID(ctx context.Context, monitorID string) error
	DeleteByTagID(ctx context.Context, tagID string) error
	DeleteByMonitorAndTag(ctx context.Context, monitorID string, tagID string) error
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
		logger.Named("[monitor-tag-service]"),
	}
}

func (s *ServiceImpl) Create(ctx context.Context, monitorID string, tagID string) (*Model, error) {
	createModel := &Model{
		MonitorID: monitorID,
		TagID:     tagID,
	}

	return s.repository.Create(ctx, createModel)
}

func (s *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return s.repository.FindByID(ctx, id)
}

func (s *ServiceImpl) Delete(ctx context.Context, id string) error {
	return s.repository.Delete(ctx, id)
}

func (s *ServiceImpl) FindByMonitorID(ctx context.Context, monitorID string) ([]*Model, error) {
	return s.repository.FindByMonitorID(ctx, monitorID)
}

func (s *ServiceImpl) FindByTagID(ctx context.Context, tagID string) ([]*Model, error) {
	return s.repository.FindByTagID(ctx, tagID)
}

func (s *ServiceImpl) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	return s.repository.DeleteByMonitorID(ctx, monitorID)
}

func (s *ServiceImpl) DeleteByTagID(ctx context.Context, tagID string) error {
	return s.repository.DeleteByTagID(ctx, tagID)
}

func (s *ServiceImpl) DeleteByMonitorAndTag(ctx context.Context, monitorID string, tagID string) error {
	return s.repository.DeleteByMonitorAndTag(ctx, monitorID, tagID)
}
