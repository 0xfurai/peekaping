package heartbeat

import (
	"context"
	"peekaping/internal/modules/events"
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
	// FindByMonitorIDPaginated lists a monitor's heartbeats from every probe
	FindByMonitorIDPaginated(ctx context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*Model, error)
	// FindByMonitorIDAndProbeIDPaginated is FindByMonitorIDPaginated narrowed to the heartbeats one
	// probe made; an empty probeID covers every probe.
	FindByMonitorIDAndProbeIDPaginated(ctx context.Context, monitorID, probeID string, limit, page int, important *bool, reverse bool) ([]*Model, error)
	DeleteByMonitorID(ctx context.Context, monitorID string) error
	HasAny(ctx context.Context) (bool, error)
}

type ServiceImpl struct {
	repository Repository
	eventBus   events.EventBus
	logger     *zap.SugaredLogger
}

func NewService(
	repository Repository,
	eventBus events.EventBus,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repository,
		eventBus,
		logger.Named("[heartbeat-service]"),
	}
}

func (mr *ServiceImpl) Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error) {
	createModel := &Model{
		MonitorID: entity.MonitorID,
		ProbeID:   entity.ProbeID,
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
	if err := events.Publish(ctx, mr.eventBus, events.HeartbeatEvent, *created); err != nil {
		mr.logger.Errorw("Failed to publish heartbeat event", "monitor_id", created.MonitorID, "error", err)
	}
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
	return mr.repository.FindByMonitorIDPaginated(ctx, monitorID, "", limit, page, important, reverse)
}

func (mr *ServiceImpl) FindByMonitorIDAndProbeIDPaginated(ctx context.Context, monitorID, probeID string, limit, page int, important *bool, reverse bool) ([]*Model, error) {
	return mr.repository.FindByMonitorIDPaginated(ctx, monitorID, probeID, limit, page, important, reverse)
}

func (mr *ServiceImpl) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	return mr.repository.DeleteByMonitorID(ctx, monitorID)
}

func (mr *ServiceImpl) HasAny(ctx context.Context) (bool, error) {
	return mr.repository.HasAny(ctx)
}
