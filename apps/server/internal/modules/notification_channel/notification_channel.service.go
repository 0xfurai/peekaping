package notification_channel

import (
	"context"
	"vigi/internal/modules/monitor_notification"

	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, entity *CreateUpdateDto, orgID string) (*Model, error)
	FindByID(ctx context.Context, id string, orgID string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string, orgID string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto, orgID string) (*Model, error)
	UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto, orgID string) (*Model, error)
	Delete(ctx context.Context, id string, orgID string) error
}

type ServiceImpl struct {
	repository                 Repository
	monitorNotificationService monitor_notification.Service
	logger                     *zap.SugaredLogger
}

func NewService(
	repository Repository,
	monitorNotificationService monitor_notification.Service,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repository,
		monitorNotificationService,
		logger.Named("[notification-service]"),
	}
}

func (mr *ServiceImpl) Create(ctx context.Context, entity *CreateUpdateDto, orgID string) (*Model, error) {
	createModel := &Model{
		OrgID:     orgID,
		Name:      entity.Name,
		Type:      entity.Type,
		Active:    entity.Active,
		IsDefault: entity.IsDefault,
		Config:    &entity.Config,
	}

	return mr.repository.Create(ctx, createModel)
}

func (mr *ServiceImpl) FindByID(ctx context.Context, id string, orgID string) (*Model, error) {
	return mr.repository.FindByID(ctx, id, orgID)
}

func (mr *ServiceImpl) FindAll(
	ctx context.Context,
	page int,
	limit int,
	q string,
	orgID string,
) ([]*Model, error) {
	entities, err := mr.repository.FindAll(ctx, page, limit, q, orgID)
	if err != nil {
		return nil, err
	}

	return entities, nil
}

func (mr *ServiceImpl) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto, orgID string) (*Model, error) {
	updateModel := &Model{
		ID:        id,
		OrgID:     orgID,
		Name:      entity.Name,
		Type:      entity.Type,
		Active:    entity.Active,
		IsDefault: entity.IsDefault,
		Config:    &entity.Config,
	}

	err := mr.repository.UpdateFull(ctx, id, updateModel, orgID)
	if err != nil {
		return nil, err
	}

	return updateModel, nil
}

func (mr *ServiceImpl) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto, orgID string) (*Model, error) {
	updateModel := &UpdateModel{
		ID:        &id,
		OrgID:     orgID,
		Name:      &entity.Name,
		Type:      &entity.Type,
		Active:    &entity.Active,
		IsDefault: &entity.IsDefault,
		Config:    &entity.Config,
	}

	err := mr.repository.UpdatePartial(ctx, id, updateModel, orgID)
	if err != nil {
		return nil, err
	}

	updatedModel, err := mr.repository.FindByID(ctx, id, orgID)
	if err != nil {
		return nil, err
	}

	return updatedModel, nil
}

func (mr *ServiceImpl) Delete(ctx context.Context, id string, orgID string) error {
	err := mr.repository.Delete(ctx, id, orgID)
	if err != nil {
		return err
	}

	// Cascade delete monitor_notification relations
	_ = mr.monitorNotificationService.DeleteByNotificationID(ctx, id)

	return nil
}
