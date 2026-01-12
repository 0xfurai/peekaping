package proxy

import (
	"context"
	"vigi/internal/modules/events"
	"vigi/internal/modules/monitor"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error)
	FindByID(ctx context.Context, id string, orgID string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string, orgID string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto, orgID string) (*Model, error)
	UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto, orgID string) (*Model, error)
	Delete(ctx context.Context, id string, orgID string) error
}

type ServiceImpl struct {
	repository     Repository
	monitorService monitor.Service
	eventBus       events.EventBus
	logger         *zap.SugaredLogger
}

type NewServiceParams struct {
	dig.In
	Repository     Repository
	MonitorService monitor.Service
	EventBus       events.EventBus
	Logger         *zap.SugaredLogger
}

func NewService(params NewServiceParams) Service {
	return &ServiceImpl{
		repository:     params.Repository,
		monitorService: params.MonitorService,
		eventBus:       params.EventBus,
		logger:         params.Logger.Named("[proxy-service]"),
	}
}

func (mr *ServiceImpl) Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error) {
	model := &Model{
		OrgID:    entity.OrgID,
		Protocol: entity.Protocol,
		Host:     entity.Host,
		Port:     entity.Port,
		Auth:     entity.Auth,
		Username: entity.Username,
		Password: entity.Password,
	}
	return mr.repository.Create(ctx, model)
}

func (mr *ServiceImpl) FindByID(ctx context.Context, id string, orgID string) (*Model, error) {
	return mr.repository.FindByID(ctx, id, orgID)
}

func (mr *ServiceImpl) FindAll(ctx context.Context, page int, limit int, q string, orgID string) ([]*Model, error) {
	return mr.repository.FindAll(ctx, page, limit, q, orgID)
}

func (mr *ServiceImpl) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto, orgID string) (*Model, error) {
	model := &Model{
		OrgID:    orgID,
		Protocol: entity.Protocol,
		Host:     entity.Host,
		Port:     entity.Port,
		Auth:     entity.Auth,
		Username: entity.Username,
		Password: entity.Password,
	}
	updated, err := mr.repository.UpdateFull(ctx, id, model, orgID)
	if err != nil {
		return nil, err
	}

	if mr.eventBus != nil {
		mr.eventBus.Publish(events.Event{
			Type:    events.ProxyUpdated,
			Payload: updated,
		})
	}

	return updated, nil
}

func (mr *ServiceImpl) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto, orgID string) (*Model, error) {
	updateModel := &UpdateModel{
		Protocol: entity.Protocol,
		Host:     entity.Host,
		Port:     entity.Port,
		Auth:     entity.Auth,
		Username: entity.Username,
		Password: entity.Password,
	}
	updated, err := mr.repository.UpdatePartial(ctx, id, updateModel, orgID)
	if err != nil {
		return nil, err
	}
	if mr.eventBus != nil {
		mr.eventBus.Publish(events.Event{
			Type:    events.ProxyUpdated,
			Payload: updated,
		})
	}
	return updated, nil
}

func (mr *ServiceImpl) Delete(ctx context.Context, id string, orgID string) error {
	_ = mr.monitorService.RemoveProxyReference(ctx, id)
	err := mr.repository.Delete(ctx, id, orgID)
	if err != nil {
		return err
	}
	if mr.eventBus != nil {
		mr.eventBus.Publish(events.Event{
			Type:    events.ProxyDeleted,
			Payload: id,
		})
	}
	return nil
}
