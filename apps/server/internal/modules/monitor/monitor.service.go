package monitor

import (
	"context"
	"fmt"
	"time"
	"vigi/internal/modules/events"
	"vigi/internal/modules/healthcheck/executor"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/monitor_notification"
	"vigi/internal/modules/monitor_tag"
	"vigi/internal/modules/shared"
	"vigi/internal/modules/stats"

	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, monitor *CreateUpdateDto) (*Model, error)
	FindByID(ctx context.Context, id string, orgID string) (*Model, error)
	FindByIDs(ctx context.Context, ids []string, orgID string) ([]*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string, active *bool, status *int, tagIds []string, orgID string) ([]*Model, error)
	FindActive(ctx context.Context) ([]*Model, error)
	FindActivePaginated(ctx context.Context, page int, limit int) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, monitor *CreateUpdateDto) (*Model, error)
	UpdatePartial(ctx context.Context, id string, monitor *PartialUpdateDto, noPublish bool, orgID string) (*Model, error)
	Delete(ctx context.Context, id string, orgID string) error
	ValidateMonitorConfig(monitorType string, configJSON string) error

	GetHeartbeats(ctx context.Context, id string, limit, page int, important *bool, reverse bool, orgID string) ([]*heartbeat.Model, error)

	RemoveProxyReference(ctx context.Context, proxyId string) error
	FindByProxyId(ctx context.Context, proxyId string) ([]*Model, error)

	GetStatPoints(ctx context.Context, id string, since, until time.Time, granularity string, orgID string) (*StatPointsSummaryDto, error)
	GetUptimeStats(ctx context.Context, id string, orgID string) (*CustomUptimeStatsDto, error)

	FindOneByPushToken(ctx context.Context, pushToken string) (*Model, error)
	ResetMonitorData(ctx context.Context, id string, orgID string) error
}

type StatPoint struct {
	Up          int     `json:"up"`
	Down        int     `json:"down"`
	Maintenance int     `json:"maintenance"`
	Ping        float64 `json:"ping"`
	PingMin     float64 `json:"ping_min"`
	PingMax     float64 `json:"ping_max"`
	Timestamp   int64   `json:"timestamp"`
}

type MonitorServiceImpl struct {
	monitorRepository          MonitorRepository
	heartbeatService           heartbeat.Service
	eventBus                   events.EventBus
	monitorNotificationService monitor_notification.Service
	monitorTagService          monitor_tag.Service
	executorRegistry           *executor.ExecutorRegistry
	statPointsService          stats.Service
	logger                     *zap.SugaredLogger
}

func NewMonitorService(
	monitorRepository MonitorRepository,
	heartbeatService heartbeat.Service,
	eventBus events.EventBus,
	monitorNotificationService monitor_notification.Service,
	monitorTagService monitor_tag.Service,
	executorRegistry *executor.ExecutorRegistry,
	statPointsService stats.Service,
	logger *zap.SugaredLogger,
) Service {
	return &MonitorServiceImpl{
		monitorRepository,
		heartbeatService,
		eventBus,
		monitorNotificationService,
		monitorTagService,
		executorRegistry,
		statPointsService,
		logger.Named("[monitor-service]"),
	}
}

func (mr *MonitorServiceImpl) Create(ctx context.Context, monitorCreateDto *CreateUpdateDto) (*Model, error) {
	createModel := &Model{
		Type:           monitorCreateDto.Type,
		Name:           monitorCreateDto.Name,
		Interval:       monitorCreateDto.Interval,
		Timeout:        monitorCreateDto.Timeout,
		MaxRetries:     monitorCreateDto.MaxRetries,
		RetryInterval:  monitorCreateDto.RetryInterval,
		ResendInterval: monitorCreateDto.ResendInterval,
		Active:         monitorCreateDto.Active,
		Status:         shared.MonitorStatusUp,
		CreatedAt:      time.Now().UTC(),
		Config:         monitorCreateDto.Config,
		ProxyId:        monitorCreateDto.ProxyId,
		PushToken:      monitorCreateDto.PushToken,
		OrgID:          monitorCreateDto.OrgID,
	}

	createdModel, err := mr.monitorRepository.Create(ctx, createModel)
	if err != nil {
		return nil, err
	}

	// Emit monitor created event
	mr.eventBus.Publish(events.Event{
		Type:    events.MonitorCreated,
		Payload: createdModel,
	})

	return createdModel, nil
}

func (mr *MonitorServiceImpl) FindByID(ctx context.Context, id string, orgID string) (*Model, error) {
	return mr.monitorRepository.FindByID(ctx, id, orgID)
}

func (mr *MonitorServiceImpl) FindByIDs(ctx context.Context, ids []string, orgID string) ([]*Model, error) {
	return mr.monitorRepository.FindByIDs(ctx, ids, orgID)
}

func (mr *MonitorServiceImpl) FindAll(ctx context.Context, page int, limit int, q string, active *bool, status *int, tagIds []string, orgID string) ([]*Model, error) {
	monitors, err := mr.monitorRepository.FindAll(ctx, page, limit, q, active, status, tagIds, orgID)
	if err != nil {
		return nil, err
	}

	return monitors, nil
}

func (mr *MonitorServiceImpl) FindActive(ctx context.Context) ([]*Model, error) {
	return mr.monitorRepository.FindActive(ctx)
}

func (mr *MonitorServiceImpl) FindActivePaginated(ctx context.Context, page int, limit int) ([]*Model, error) {
	return mr.monitorRepository.FindActivePaginated(ctx, page, limit)
}

func (mr *MonitorServiceImpl) UpdateFull(ctx context.Context, id string, monitor *CreateUpdateDto) (*Model, error) {
	model := &Model{
		ID:             id,
		Name:           monitor.Name,
		Type:           monitor.Type,
		Interval:       monitor.Interval,
		Timeout:        monitor.Timeout,
		MaxRetries:     monitor.MaxRetries,
		RetryInterval:  monitor.RetryInterval,
		ResendInterval: monitor.ResendInterval,
		Active:         monitor.Active,
		Status:         shared.MonitorStatusUp,
		UpdatedAt:      time.Now().UTC(),
		Config:         monitor.Config,
		ProxyId:        monitor.ProxyId,
		PushToken:      monitor.PushToken,
		OrgID:          monitor.OrgID,
	}

	err := mr.monitorRepository.UpdateFull(ctx, id, model, monitor.OrgID)
	if err != nil {
		return nil, err
	}

	// Emit monitor updated event
	mr.eventBus.Publish(events.Event{
		Type:    events.MonitorUpdated,
		Payload: model,
	})

	return model, nil
}

func (mr *MonitorServiceImpl) UpdatePartial(ctx context.Context, id string, monitor *PartialUpdateDto, noPublish bool, orgID string) (*Model, error) {
	model := &UpdateModel{
		ID:             &id,
		Type:           monitor.Type,
		Name:           monitor.Name,
		Interval:       monitor.Interval,
		Timeout:        monitor.Timeout,
		MaxRetries:     monitor.MaxRetries,
		RetryInterval:  monitor.RetryInterval,
		ResendInterval: monitor.ResendInterval,
		Active:         monitor.Active,
		Status:         monitor.Status,
		Config:         monitor.Config,
		ProxyId:        monitor.ProxyId,
		PushToken:      monitor.PushToken,
		OrgID:          monitor.OrgID,
	}

	err := mr.monitorRepository.UpdatePartial(ctx, id, model, orgID)
	if err != nil {
		return nil, err
	}

	// Get the updated monitor
	updatedMonitor, err := mr.FindByID(ctx, id, orgID)
	if err != nil {
		return nil, err
	}

	// Emit monitor updated event
	if !noPublish {
		mr.eventBus.Publish(events.Event{
			Type:    events.MonitorUpdated,
			Payload: updatedMonitor,
		})
	}

	return updatedMonitor, nil
}

func (mr *MonitorServiceImpl) Delete(ctx context.Context, id string, orgID string) error {
	err := mr.monitorRepository.Delete(ctx, id, orgID)
	if err != nil {
		return err
	}

	_ = mr.monitorNotificationService.DeleteByMonitorID(ctx, id)
	_ = mr.monitorTagService.DeleteByMonitorID(ctx, id)
	_ = mr.heartbeatService.DeleteByMonitorID(ctx, id)
	_ = mr.statPointsService.DeleteByMonitorID(ctx, id)

	// Emit monitor deleted event
	mr.eventBus.Publish(events.Event{
		Type:    events.MonitorDeleted,
		Payload: id,
	})

	return nil
}

func (mr *MonitorServiceImpl) ValidateMonitorConfig(
	monitorType string,
	configJSON string,
) error {
	if mr.executorRegistry == nil {
		return fmt.Errorf("executor registry not available")
	}
	return mr.executorRegistry.ValidateConfig(monitorType, configJSON)
}

func (mr *MonitorServiceImpl) GetHeartbeats(ctx context.Context, id string, limit, page int, important *bool, reverse bool, orgID string) ([]*heartbeat.Model, error) {
	// First check ownership
	_, err := mr.FindByID(ctx, id, orgID)
	if err != nil {
		return nil, err
	}

	return mr.heartbeatService.FindByMonitorIDPaginated(ctx, id, limit, page, important, reverse)
}

func (mr *MonitorServiceImpl) RemoveProxyReference(ctx context.Context, proxyId string) error {
	return mr.monitorRepository.RemoveProxyReference(ctx, proxyId)
}

func (mr *MonitorServiceImpl) FindByProxyId(ctx context.Context, proxyId string) ([]*Model, error) {
	return mr.monitorRepository.FindByProxyId(ctx, proxyId)
}

func (mr *MonitorServiceImpl) GetStatPoints(ctx context.Context, id string, since, until time.Time, granularity string, orgID string) (*StatPointsSummaryDto, error) {
	var period stats.StatPeriod
	switch granularity {
	case "minute":
		period = stats.StatMinutely
	case "hour":
		period = stats.StatHourly
	case "day":
		period = stats.StatDaily
	default:
		return nil, fmt.Errorf("invalid granularity: %s", granularity)
	}

	// Get monitor information to pass interval for minute-level grouping
	monitor, err := mr.FindByID(ctx, id, orgID)
	if err != nil {
		return nil, err
	}
	if monitor == nil {
		return nil, fmt.Errorf("monitor not found")
	}

	// Use the new method that accepts monitor interval
	statsList, err := mr.statPointsService.FindStatsByMonitorIDAndTimeRangeWithInterval(ctx, id, since, until, period, monitor.Interval)
	if err != nil {
		return nil, err
	}

	points := make([]*StatPoint, 0, len(statsList))
	for _, s := range statsList {
		points = append(points, &StatPoint{
			Up:          s.Up,
			Down:        s.Down,
			Maintenance: s.Maintenance,
			Ping:        s.Ping,
			PingMin:     s.PingMin,
			PingMax:     s.PingMax,
			Timestamp:   s.Timestamp.Unix() * 1000,
		})
	}

	stats := mr.statPointsService.StatPointsSummary(statsList)

	return &StatPointsSummaryDto{
		Points:  points,
		MaxPing: stats.MaxPing,
		MinPing: stats.MinPing,
		AvgPing: stats.AvgPing,
		Uptime:  stats.Uptime,
	}, nil
}

// GetCustomUptimeStatsShort returns uptime percentages for 24h, 30d, 365d
func (mr *MonitorServiceImpl) GetUptimeStats(ctx context.Context, id string, orgID string) (*CustomUptimeStatsDto, error) {
	// Check ownership
	_, err := mr.FindByID(ctx, id, orgID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	periods := map[string]time.Duration{
		"24h":  24 * time.Hour,
		"7d":   7 * 24 * time.Hour,
		"30d":  30 * 24 * time.Hour,
		"365d": 365 * 24 * time.Hour,
	}

	uptimes := make(map[string]float64)
	for key, duration := range periods {
		since := now.Add(-duration)
		statsList, err := mr.statPointsService.FindStatsByMonitorIDAndTimeRange(ctx, id, since, now, stats.StatDaily)
		if err != nil {
			return nil, err
		}
		summary := mr.statPointsService.StatPointsSummary(statsList)
		uptime := 0.0
		if summary.Uptime != nil {
			uptime = *summary.Uptime
		}
		uptimes[key] = uptime
	}

	stats := &CustomUptimeStatsDto{
		Uptime24h:  uptimes["24h"],
		Uptime7d:   uptimes["7d"],
		Uptime30d:  uptimes["30d"],
		Uptime365d: uptimes["365d"],
	}

	return stats, nil
}

func (mr *MonitorServiceImpl) FindOneByPushToken(ctx context.Context, pushToken string) (*Model, error) {
	return mr.monitorRepository.FindOneByPushToken(ctx, pushToken)
}

func (mr *MonitorServiceImpl) ResetMonitorData(ctx context.Context, id string, orgID string) error {
	// First check if monitor exists
	monitor, err := mr.monitorRepository.FindByID(ctx, id, orgID)
	if err != nil {
		return err
	}
	if monitor == nil {
		return fmt.Errorf("monitor not found")
	}

	// Delete all heartbeats for this monitor
	err = mr.heartbeatService.DeleteByMonitorID(ctx, id)
	if err != nil {
		mr.logger.Errorw("Failed to delete heartbeats for monitor", "monitorID", id, "error", err)
		return fmt.Errorf("failed to delete heartbeats: %w", err)
	}

	// Delete all stats for this monitor
	err = mr.statPointsService.DeleteByMonitorID(ctx, id)
	if err != nil {
		mr.logger.Errorw("Failed to delete stats for monitor", "monitorID", id, "error", err)
		return fmt.Errorf("failed to delete stats: %w", err)
	}

	// Reset monitor status to pending (like a fresh monitor)
	pendingStatus := shared.MonitorStatusPending
	err = mr.monitorRepository.UpdatePartial(ctx, id, &UpdateModel{
		ID:     &id,
		Status: &pendingStatus,
	}, orgID)
	if err != nil {
		mr.logger.Errorw("Failed to reset monitor status", "monitorID", id, "error", err)
		return fmt.Errorf("failed to reset monitor status: %w", err)
	}

	mr.logger.Infow("Successfully reset monitor data", "monitorID", id)

	// Emit monitor updated event
	updatedMonitor, _ := mr.FindByID(ctx, id, orgID)
	if updatedMonitor != nil {
		mr.eventBus.Publish(events.Event{
			Type:    events.MonitorUpdated,
			Payload: updatedMonitor,
		})
	} else {
		mr.logger.Errorw("Failed to find updated monitor", "monitorID", id)
	}

	return nil
}
