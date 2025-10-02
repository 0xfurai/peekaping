package proxy

import (
	"context"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"

	"go.uber.org/dig"
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
		Protocol: entity.Protocol,
		Host:     entity.Host,
		Port:     entity.Port,
		Auth:     entity.Auth,
		Username: entity.Username,
		Password: entity.Password,
	}
	return mr.repository.Create(ctx, model)
}

func (mr *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return mr.repository.FindByID(ctx, id)
}

func (mr *ServiceImpl) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	return mr.repository.FindAll(ctx, page, limit, q)
}

func (mr *ServiceImpl) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error) {
	model := &Model{
		Protocol: entity.Protocol,
		Host:     entity.Host,
		Port:     entity.Port,
		Auth:     entity.Auth,
		Username: entity.Username,
		Password: entity.Password,
	}
	updated, err := mr.repository.UpdateFull(ctx, id, model)
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

func (mr *ServiceImpl) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error) {
	updateModel := &UpdateModel{
		Protocol: entity.Protocol,
		Host:     entity.Host,
		Port:     entity.Port,
		Auth:     entity.Auth,
		Username: entity.Username,
		Password: entity.Password,
	}
	updated, err := mr.repository.UpdatePartial(ctx, id, updateModel)
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

func (mr *ServiceImpl) Delete(ctx context.Context, id string) error {
	_ = mr.monitorService.RemoveProxyReference(ctx, id)
	err := mr.repository.Delete(ctx, id)
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
