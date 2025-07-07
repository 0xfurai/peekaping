package heartbeat

import (
	"context"
	"peekaping/src/modules/events"
	"peekaping/src/modules/stats"
	"time"

	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int) ([]*Model, error)
	Delete(ctx context.Context, id string) error

	FindUptimeStatsByMonitorID(ctx context.Context, monitorID string, periods map[string]time.Duration, now time.Time) (map[string]float64, error)
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
	FindByMonitorIDPaginated(ctx context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*Model, error)
	DeleteByMonitorID(ctx context.Context, monitorID string) error
}

type ServiceImpl struct {
	repository   Repository
	statsService stats.Service
	eventBus     *events.EventBus
	logger       *zap.SugaredLogger
}

func NewService(
	repository Repository,
	statsService stats.Service,
	eventBus *events.EventBus,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repository,
		statsService,
		eventBus,
		logger.Named("[heartbeat-service]"),
	}
}

func (mr *ServiceImpl) Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error) {
	createModel := &Model{
		MonitorID: entity.MonitorID,
		Status:    entity.Status,
		Msg:       entity.Msg,
		Ping:      entity.Ping,
		Duration:  entity.Duration,
		DownCount: entity.DownCount,
		Retries:   entity.Retries,
		Important: entity.Important,
		Time:      entity.Time,
		EndTime:   entity.EndTime,
		Notified:  entity.Notified,
	}

	created, err := mr.repository.Create(ctx, createModel)
	if err != nil {
		return nil, err
	}
	// Emit HeartbeatCreated event
	mr.eventBus.Publish(events.Event{
		Type:    events.HeartbeatEvent,
		Payload: created,
	})
	return created, nil
}

func (mr *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return mr.repository.FindByID(ctx, id)
}

func (mr *ServiceImpl) FindAll(ctx context.Context, page int, limit int) ([]*Model, error) {
	// Call the repository's FindAll method to retrieve paginated monitors
	monitors, err := mr.repository.FindAll(ctx, page, limit)
	if err != nil {
		return nil, err
	}

	return monitors, nil
}

func (mr *ServiceImpl) FindActive(ctx context.Context) ([]*Model, error) {
	return mr.repository.FindActive(ctx)
}

func (mr *ServiceImpl) Delete(ctx context.Context, id string) error {
	return mr.repository.Delete(ctx, id)
}

func (mr *ServiceImpl) FindUptimeStatsByMonitorID(ctx context.Context, monitorID string, periods map[string]time.Duration, now time.Time) (map[string]float64, error) {
	return mr.repository.FindUptimeStatsByMonitorID(ctx, monitorID, periods, now)
}

func (mr *ServiceImpl) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	return mr.repository.DeleteOlderThan(ctx, cutoff)
}

func (mr *ServiceImpl) FindByMonitorIDPaginated(ctx context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*Model, error) {
	return mr.repository.FindByMonitorIDPaginated(ctx, monitorID, limit, page, important, reverse)
}

func (mr *ServiceImpl) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	return mr.repository.DeleteByMonitorID(ctx, monitorID)
}
