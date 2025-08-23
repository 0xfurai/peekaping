package monitor_status_page

import (
	"context"

	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error)
	UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error)
	Delete(ctx context.Context, id string) error

	// Additional methods for managing relationships
	AddMonitorToStatusPage(ctx context.Context, statusPageID, monitorID string, order int, active bool) (*Model, error)
	RemoveMonitorFromStatusPage(ctx context.Context, statusPageID, monitorID string) error
	GetMonitorsForStatusPage(ctx context.Context, statusPageID string) ([]*Model, error)
	GetStatusPagesForMonitor(ctx context.Context, monitorID string) ([]*Model, error)
	FindByStatusPageAndMonitor(ctx context.Context, statusPageID, monitorID string) (*Model, error)
	UpdateMonitorOrder(ctx context.Context, statusPageID, monitorID string, order int) (*Model, error)
	UpdateMonitorActiveStatus(ctx context.Context, statusPageID, monitorID string, active bool) (*Model, error)
	DeleteAllMonitorsForStatusPage(ctx context.Context, statusPageID string) error
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
		logger.Named("[monitor-status-page-service]"),
	}
}

func (mr *ServiceImpl) Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error) {
	return mr.repository.Create(ctx, entity)
}

func (mr *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return mr.repository.FindByID(ctx, id)
}

func (mr *ServiceImpl) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	return mr.repository.FindAll(ctx, page, limit, q)
}

func (mr *ServiceImpl) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error) {
	return mr.repository.UpdateFull(ctx, id, entity)
}

func (mr *ServiceImpl) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error) {
	return mr.repository.UpdatePartial(ctx, id, entity)
}

func (mr *ServiceImpl) Delete(ctx context.Context, id string) error {
	return mr.repository.Delete(ctx, id)
}

func (mr *ServiceImpl) AddMonitorToStatusPage(ctx context.Context, statusPageID, monitorID string, order int, active bool) (*Model, error) {
	mr.logger.Debugw("Adding monitor to status page", "statusPageID", statusPageID, "monitorID", monitorID, "order", order, "active", active)
	return mr.repository.AddMonitorToStatusPage(ctx, statusPageID, monitorID, order, active)
}

func (mr *ServiceImpl) RemoveMonitorFromStatusPage(ctx context.Context, statusPageID, monitorID string) error {
	return mr.repository.RemoveMonitorFromStatusPage(ctx, statusPageID, monitorID)
}

func (mr *ServiceImpl) GetMonitorsForStatusPage(ctx context.Context, statusPageID string) ([]*Model, error) {
	return mr.repository.GetMonitorsForStatusPage(ctx, statusPageID)
}

func (mr *ServiceImpl) GetStatusPagesForMonitor(ctx context.Context, monitorID string) ([]*Model, error) {
	return mr.repository.GetStatusPagesForMonitor(ctx, monitorID)
}

func (mr *ServiceImpl) FindByStatusPageAndMonitor(ctx context.Context, statusPageID, monitorID string) (*Model, error) {
	return mr.repository.FindByStatusPageAndMonitor(ctx, statusPageID, monitorID)
}

func (mr *ServiceImpl) UpdateMonitorOrder(ctx context.Context, statusPageID, monitorID string, order int) (*Model, error) {
	return mr.repository.UpdateMonitorOrder(ctx, statusPageID, monitorID, order)
}

func (mr *ServiceImpl) UpdateMonitorActiveStatus(ctx context.Context, statusPageID, monitorID string, active bool) (*Model, error) {
	return mr.repository.UpdateMonitorActiveStatus(ctx, statusPageID, monitorID, active)
}

func (mr *ServiceImpl) DeleteAllMonitorsForStatusPage(ctx context.Context, statusPageID string) error {
	return mr.repository.DeleteAllMonitorsForStatusPage(ctx, statusPageID)
}
