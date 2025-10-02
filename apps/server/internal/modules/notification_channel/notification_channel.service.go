package notification_channel

import (
	"context"
	"peekaping/internal/modules/monitor_notification"

	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error)
	UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error)
	Delete(ctx context.Context, id string) error
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

func (mr *ServiceImpl) Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error) {
	createModel := &Model{
		Name:      entity.Name,
		Type:      entity.Type,
		Active:    entity.Active,
		IsDefault: entity.IsDefault,
		Config:    &entity.Config,
	}

	return mr.repository.Create(ctx, createModel)
}

func (mr *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return mr.repository.FindByID(ctx, id)
}

func (mr *ServiceImpl) FindAll(
	ctx context.Context,
	page int,
	limit int,
	q string,
) ([]*Model, error) {
	entities, err := mr.repository.FindAll(ctx, page, limit, q)
	if err != nil {
		return nil, err
	}

	return entities, nil
}

func (mr *ServiceImpl) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error) {
	updateModel := &Model{
		ID:        id,
		Name:      entity.Name,
		Type:      entity.Type,
		Active:    entity.Active,
		IsDefault: entity.IsDefault,
		Config:    &entity.Config,
	}

	err := mr.repository.UpdateFull(ctx, id, updateModel)
	if err != nil {
		return nil, err
	}

	return updateModel, nil
}

func (mr *ServiceImpl) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error) {
	updateModel := &UpdateModel{
		ID:        &id,
		Name:      &entity.Name,
		Type:      &entity.Type,
		Active:    &entity.Active,
		IsDefault: &entity.IsDefault,
		Config:    &entity.Config,
	}

	err := mr.repository.UpdatePartial(ctx, id, updateModel)
	if err != nil {
		return nil, err
	}

	updatedModel, err := mr.repository.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return updatedModel, nil
}

func (mr *ServiceImpl) Delete(ctx context.Context, id string) error {
	err := mr.repository.Delete(ctx, id)
	if err != nil {
		return err
	}

	// Cascade delete monitor_notification relations
	_ = mr.monitorNotificationService.DeleteByNotificationID(ctx, id)

	return nil
}
