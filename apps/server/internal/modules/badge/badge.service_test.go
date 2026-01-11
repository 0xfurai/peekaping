package badge

import (
	"context"
	"errors"
	"testing"
	"time"
	"vigi/internal/modules/events"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/monitor"
	"vigi/internal/modules/monitor_status_page"
	"vigi/internal/modules/monitor_tls_info"
	"vigi/internal/modules/shared"
	"vigi/internal/modules/stats"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// Mock implementations for all dependencies

type MockMonitorService struct {
	mock.Mock
}

func (m *MockMonitorService) Create(ctx context.Context, monitor *monitor.CreateUpdateDto) (*shared.Monitor, error) {
	args := m.Called(ctx, monitor)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) FindByID(ctx context.Context, id string, orgID string) (*shared.Monitor, error) {
	args := m.Called(ctx, id, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) FindByIDs(ctx context.Context, ids []string, orgID string) ([]*shared.Monitor, error) {
	args := m.Called(ctx, ids, orgID)
	return args.Get(0).([]*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) FindAll(ctx context.Context, page int, limit int, q string, active *bool, status *int, tagIds []string, orgID string) ([]*shared.Monitor, error) {
	args := m.Called(ctx, page, limit, q, active, status, tagIds, orgID)
	return args.Get(0).([]*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) FindActive(ctx context.Context) ([]*shared.Monitor, error) {
	args := m.Called(ctx)
	return args.Get(0).([]*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) UpdateFull(ctx context.Context, id string, monitor *monitor.CreateUpdateDto) (*shared.Monitor, error) {
	args := m.Called(ctx, id, monitor)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) UpdatePartial(ctx context.Context, id string, monitor *monitor.PartialUpdateDto, noPublish bool, orgID string) (*shared.Monitor, error) {
	args := m.Called(ctx, id, monitor, noPublish, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) Delete(ctx context.Context, id string, orgID string) error {
	args := m.Called(ctx, id, orgID)
	return args.Error(0)
}

func (m *MockMonitorService) ValidateMonitorConfig(monitorType string, configJSON string) error {
	args := m.Called(monitorType, configJSON)
	return args.Error(0)
}

func (m *MockMonitorService) GetHeartbeats(ctx context.Context, id string, limit, page int, important *bool, reverse bool, orgID string) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, id, limit, page, important, reverse, orgID)
	return args.Get(0).([]*heartbeat.Model), args.Error(1)
}

func (m *MockMonitorService) RemoveProxyReference(ctx context.Context, proxyId string) error {
	args := m.Called(ctx, proxyId)
	return args.Error(0)
}

func (m *MockMonitorService) FindByProxyId(ctx context.Context, proxyId string) ([]*shared.Monitor, error) {
	args := m.Called(ctx, proxyId)
	return args.Get(0).([]*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) GetStatPoints(ctx context.Context, id string, since, until time.Time, granularity string, orgID string) (*monitor.StatPointsSummaryDto, error) {
	args := m.Called(ctx, id, since, until, granularity, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor.StatPointsSummaryDto), args.Error(1)
}

func (m *MockMonitorService) GetUptimeStats(ctx context.Context, id string, orgID string) (*monitor.CustomUptimeStatsDto, error) {
	args := m.Called(ctx, id, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor.CustomUptimeStatsDto), args.Error(1)
}

func (m *MockMonitorService) FindOneByPushToken(ctx context.Context, pushToken string) (*shared.Monitor, error) {
	args := m.Called(ctx, pushToken)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) ResetMonitorData(ctx context.Context, id string, orgID string) error {
	args := m.Called(ctx, id, orgID)
	return args.Error(0)
}

func (m *MockMonitorService) FindActivePaginated(ctx context.Context, page int, limit int) ([]*shared.Monitor, error) {
	args := m.Called(ctx, page, limit)
	return args.Get(0).([]*shared.Monitor), args.Error(1)
}

type MockHeartbeatService struct {
	mock.Mock
}

func (m *MockHeartbeatService) Create(ctx context.Context, entity *heartbeat.CreateUpdateDto) (*heartbeat.Model, error) {
	args := m.Called(ctx, entity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*heartbeat.Model), args.Error(1)
}

func (m *MockHeartbeatService) FindByID(ctx context.Context, id string) (*heartbeat.Model, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*heartbeat.Model), args.Error(1)
}

func (m *MockHeartbeatService) FindAll(ctx context.Context, page int, limit int) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, page, limit)
	return args.Get(0).([]*heartbeat.Model), args.Error(1)
}

func (m *MockHeartbeatService) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockHeartbeatService) FindUptimeStatsByMonitorID(ctx context.Context, monitorID string, periods map[string]time.Duration, now time.Time) (map[string]float64, error) {
	args := m.Called(ctx, monitorID, periods, now)
	return args.Get(0).(map[string]float64), args.Error(1)
}

func (m *MockHeartbeatService) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	args := m.Called(ctx, cutoff)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockHeartbeatService) FindByMonitorIDPaginated(ctx context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, monitorID, limit, page, important, reverse)
	return args.Get(0).([]*heartbeat.Model), args.Error(1)
}

func (m *MockHeartbeatService) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	args := m.Called(ctx, monitorID)
	return args.Error(0)
}

type MockStatsService struct {
	mock.Mock
}

func (m *MockStatsService) AggregateHeartbeat(ctx context.Context, hb *stats.HeartbeatPayload) error {
	args := m.Called(ctx, hb)
	return args.Error(0)
}

func (m *MockStatsService) RegisterEventHandlers(eventBus events.EventBus) {
	m.Called(eventBus)
}

func (m *MockStatsService) FindStatsByMonitorIDAndTimeRange(ctx context.Context, monitorID string, since, until time.Time, period stats.StatPeriod) ([]*stats.Stat, error) {
	args := m.Called(ctx, monitorID, since, until, period)
	return args.Get(0).([]*stats.Stat), args.Error(1)
}

func (m *MockStatsService) FindStatsByMonitorIDAndTimeRangeWithInterval(ctx context.Context, monitorID string, since, until time.Time, period stats.StatPeriod, monitorInterval int) ([]*stats.Stat, error) {
	args := m.Called(ctx, monitorID, since, until, period, monitorInterval)
	return args.Get(0).([]*stats.Stat), args.Error(1)
}

func (m *MockStatsService) StatPointsSummary(statsList []*stats.Stat) *stats.Stats {
	args := m.Called(statsList)
	return args.Get(0).(*stats.Stats)
}

func (m *MockStatsService) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	args := m.Called(ctx, monitorID)
	return args.Error(0)
}

type MockTLSInfoService struct {
	mock.Mock
}

func (m *MockTLSInfoService) GetTLSInfo(ctx context.Context, monitorID string) (*monitor_tls_info.TLSInfo, error) {
	args := m.Called(ctx, monitorID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor_tls_info.TLSInfo), args.Error(1)
}

func (m *MockTLSInfoService) StoreTLSInfo(ctx context.Context, monitorID string, infoJSON string) error {
	args := m.Called(ctx, monitorID, infoJSON)
	return args.Error(0)
}

func (m *MockTLSInfoService) StoreTLSInfoObject(ctx context.Context, monitorID string, info interface{}) error {
	args := m.Called(ctx, monitorID, info)
	return args.Error(0)
}

func (m *MockTLSInfoService) GetTLSInfoObject(ctx context.Context, monitorID string, obj interface{}) error {
	args := m.Called(ctx, monitorID, obj)
	return args.Error(0)
}

func (m *MockTLSInfoService) DeleteTLSInfo(ctx context.Context, monitorID string) error {
	args := m.Called(ctx, monitorID)
	return args.Error(0)
}

func (m *MockTLSInfoService) CleanupOldRecords(ctx context.Context, olderThanDays int) error {
	args := m.Called(ctx, olderThanDays)
	return args.Error(0)
}

type MockMonitorStatusPageService struct {
	mock.Mock
}

func (m *MockMonitorStatusPageService) Create(ctx context.Context, entity *monitor_status_page.CreateUpdateDto) (*monitor_status_page.Model, error) {
	args := m.Called(ctx, entity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor_status_page.Model), args.Error(1)
}

func (m *MockMonitorStatusPageService) FindByID(ctx context.Context, id string) (*monitor_status_page.Model, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor_status_page.Model), args.Error(1)
}

func (m *MockMonitorStatusPageService) FindAll(ctx context.Context, page int, limit int, q string) ([]*monitor_status_page.Model, error) {
	args := m.Called(ctx, page, limit, q)
	return args.Get(0).([]*monitor_status_page.Model), args.Error(1)
}

func (m *MockMonitorStatusPageService) UpdateFull(ctx context.Context, id string, entity *monitor_status_page.CreateUpdateDto) (*monitor_status_page.Model, error) {
	args := m.Called(ctx, id, entity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor_status_page.Model), args.Error(1)
}

func (m *MockMonitorStatusPageService) UpdatePartial(ctx context.Context, id string, entity *monitor_status_page.PartialUpdateDto) (*monitor_status_page.Model, error) {
	args := m.Called(ctx, id, entity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor_status_page.Model), args.Error(1)
}

func (m *MockMonitorStatusPageService) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockMonitorStatusPageService) AddMonitorToStatusPage(ctx context.Context, statusPageID, monitorID string, order int, active bool) (*monitor_status_page.Model, error) {
	args := m.Called(ctx, statusPageID, monitorID, order, active)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor_status_page.Model), args.Error(1)
}

func (m *MockMonitorStatusPageService) RemoveMonitorFromStatusPage(ctx context.Context, statusPageID, monitorID string) error {
	args := m.Called(ctx, statusPageID, monitorID)
	return args.Error(0)
}

func (m *MockMonitorStatusPageService) GetMonitorsForStatusPage(ctx context.Context, statusPageID string) ([]*monitor_status_page.Model, error) {
	args := m.Called(ctx, statusPageID)
	return args.Get(0).([]*monitor_status_page.Model), args.Error(1)
}

func (m *MockMonitorStatusPageService) GetStatusPagesForMonitor(ctx context.Context, monitorID string) ([]*monitor_status_page.Model, error) {
	args := m.Called(ctx, monitorID)
	return args.Get(0).([]*monitor_status_page.Model), args.Error(1)
}

func (m *MockMonitorStatusPageService) FindByStatusPageAndMonitor(ctx context.Context, statusPageID, monitorID string) (*monitor_status_page.Model, error) {
	args := m.Called(ctx, statusPageID, monitorID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor_status_page.Model), args.Error(1)
}

func (m *MockMonitorStatusPageService) UpdateMonitorOrder(ctx context.Context, statusPageID, monitorID string, order int) (*monitor_status_page.Model, error) {
	args := m.Called(ctx, statusPageID, monitorID, order)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor_status_page.Model), args.Error(1)
}

func (m *MockMonitorStatusPageService) UpdateMonitorActiveStatus(ctx context.Context, statusPageID, monitorID string, active bool) (*monitor_status_page.Model, error) {
	args := m.Called(ctx, statusPageID, monitorID, active)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor_status_page.Model), args.Error(1)
}

func (m *MockMonitorStatusPageService) DeleteAllMonitorsForStatusPage(ctx context.Context, statusPageID string) error {
	args := m.Called(ctx, statusPageID)
	return args.Error(0)
}

// Test setup helper
func setupBadgeService() (*ServiceImpl, *MockMonitorService, *MockHeartbeatService, *MockStatsService, *MockTLSInfoService, *MockMonitorStatusPageService) {
	mockMonitorService := &MockMonitorService{}
	mockHeartbeatService := &MockHeartbeatService{}
	mockStatsService := &MockStatsService{}
	mockTLSInfoService := &MockTLSInfoService{}
	mockMonitorStatusPageService := &MockMonitorStatusPageService{}
	logger := zap.NewNop().Sugar()

	service := NewService(
		mockMonitorService,
		mockHeartbeatService,
		mockStatsService,
		mockTLSInfoService,
		mockMonitorStatusPageService,
		logger,
	).(*ServiceImpl)

	return service, mockMonitorService, mockHeartbeatService, mockStatsService, mockTLSInfoService, mockMonitorStatusPageService
}

// Helper function tests

func TestGetLabel(t *testing.T) {
	tests := []struct {
		name         string
		label        string
		defaultLabel string
		expected     string
	}{
		{
			name:         "empty label returns default",
			label:        "",
			defaultLabel: "default",
			expected:     "default",
		},
		{
			name:         "non-empty label returns label",
			label:        "custom",
			defaultLabel: "default",
			expected:     "custom",
		},
		{
			name:         "whitespace label returns default",
			label:        "   ",
			defaultLabel: "default",
			expected:     "   ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getLabel(tt.label, tt.defaultLabel)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewService(t *testing.T) {
	mockMonitorService := &MockMonitorService{}
	mockHeartbeatService := &MockHeartbeatService{}
	mockStatsService := &MockStatsService{}
	mockTLSInfoService := &MockTLSInfoService{}
	mockMonitorStatusPageService := &MockMonitorStatusPageService{}
	logger := zap.NewNop().Sugar()

	service := NewService(
		mockMonitorService,
		mockHeartbeatService,
		mockStatsService,
		mockTLSInfoService,
		mockMonitorStatusPageService,
		logger,
	)

	assert.NotNil(t, service)
	assert.IsType(t, &ServiceImpl{}, service)

	serviceImpl := service.(*ServiceImpl)
	assert.Equal(t, mockMonitorService, serviceImpl.monitorService)
	assert.Equal(t, mockHeartbeatService, serviceImpl.heartbeatService)
	assert.Equal(t, mockStatsService, serviceImpl.statsService)
	assert.Equal(t, mockTLSInfoService, serviceImpl.tlsInfoService)
	assert.Equal(t, mockMonitorStatusPageService, serviceImpl.monitorStatusPageService)
	assert.NotNil(t, serviceImpl.svgGenerator)
	assert.NotNil(t, serviceImpl.logger)
}

// IsMonitorPublic tests

func TestServiceImpl_IsMonitorPublic(t *testing.T) {
	ctx := context.Background()

	t.Run("monitor is public when on status page", func(t *testing.T) {
		service, mockMonitorService, _, _, _, mockStatusPageService := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Active: true,
		}

		statusPages := []*monitor_status_page.Model{
			{ID: "page1", MonitorID: monitorID},
		}

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockStatusPageService.On("GetStatusPagesForMonitor", ctx, monitorID).Return(statusPages, nil)

		result, err := service.IsMonitorPublic(ctx, monitorID)

		assert.NoError(t, err)
		assert.True(t, result)
		mockMonitorService.AssertExpectations(t)
		mockStatusPageService.AssertExpectations(t)
	})

	t.Run("monitor is not public when not on any status page", func(t *testing.T) {
		service, mockMonitorService, _, _, _, mockStatusPageService := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Active: true,
		}

		statusPages := []*monitor_status_page.Model{}

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockStatusPageService.On("GetStatusPagesForMonitor", ctx, monitorID).Return(statusPages, nil)

		result, err := service.IsMonitorPublic(ctx, monitorID)

		assert.NoError(t, err)
		assert.False(t, result)
		mockMonitorService.AssertExpectations(t)
		mockStatusPageService.AssertExpectations(t)
	})

	t.Run("monitor not found", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "nonexistent"

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(nil, errors.New("not found"))

		result, err := service.IsMonitorPublic(ctx, monitorID)

		assert.Error(t, err)
		assert.False(t, result)
		assert.Contains(t, err.Error(), "not found")
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("monitor is nil", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "monitor123"

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(nil, nil)

		result, err := service.IsMonitorPublic(ctx, monitorID)

		assert.NoError(t, err)
		assert.False(t, result)
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("monitor is inactive", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Active: false,
		}

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)

		result, err := service.IsMonitorPublic(ctx, monitorID)

		assert.NoError(t, err)
		assert.False(t, result)
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("status page check fails but returns true", func(t *testing.T) {
		service, mockMonitorService, _, _, _, mockStatusPageService := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Active: true,
		}

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockStatusPageService.On("GetStatusPagesForMonitor", ctx, monitorID).Return([]*monitor_status_page.Model{}, errors.New("status page error"))

		result, err := service.IsMonitorPublic(ctx, monitorID)

		assert.NoError(t, err)
		assert.True(t, result) // Should return true when status page check fails
		mockMonitorService.AssertExpectations(t)
		mockStatusPageService.AssertExpectations(t)
	})
}

// GetMonitorBadgeData tests

func TestServiceImpl_GetMonitorBadgeData(t *testing.T) {
	ctx := context.Background()

	t.Run("successful badge data retrieval", func(t *testing.T) {
		service, mockMonitorService, mockHeartbeatService, mockStatsService, mockTLSInfoService, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		_ = time.Now().UTC()
		uptime24h := 99.5
		avgPing24h := 150.0
		uptime30d := 98.2
		avgPing30d := 180.0
		uptime90d := 97.8
		avgPing90d := 200.0
		lastPing := 120

		// Mock stats responses
		stats24h := []*stats.Stat{{}}
		stats30d := []*stats.Stat{{}}
		stats90d := []*stats.Stat{{}}

		summary24h := &stats.Stats{
			Uptime:  &uptime24h,
			AvgPing: &avgPing24h,
		}
		summary30d := &stats.Stats{
			Uptime:  &uptime30d,
			AvgPing: &avgPing30d,
		}
		summary90d := &stats.Stats{
			Uptime:  &uptime90d,
			AvgPing: &avgPing90d,
		}

		heartbeats := []*heartbeat.Model{
			{
				ID:        "hb1",
				MonitorID: monitorID,
				Ping:      lastPing,
			},
		}

		expiryDays := 30
		expiryDate := time.Now().AddDate(0, 0, expiryDays)
		tlsInfo := &monitor_tls_info.TLSInfo{
			CertInfo: &shared.CertificateInfo{
				DaysRemaining: expiryDays,
				ValidTo:       expiryDate,
			},
		}

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatHourly).Return(stats24h, nil).Once()
		mockStatsService.On("StatPointsSummary", stats24h).Return(summary24h).Once()
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatDaily).Return(stats30d, nil).Once()
		mockStatsService.On("StatPointsSummary", stats30d).Return(summary30d).Once()
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatDaily).Return(stats90d, nil).Once()
		mockStatsService.On("StatPointsSummary", stats90d).Return(summary90d).Once()
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, monitorID, 1, 0, (*bool)(nil), true).Return(heartbeats, nil)
		mockTLSInfoService.On("GetTLSInfo", ctx, monitorID).Return(tlsInfo, nil)

		result, err := service.GetMonitorBadgeData(ctx, monitorID)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, monitorID, result.ID)
		assert.Equal(t, "Test Monitor", result.Name)
		assert.Equal(t, int(shared.MonitorStatusUp), result.Status)
		assert.True(t, result.Active)
		assert.Equal(t, &uptime24h, result.Uptime24h)
		assert.Equal(t, &avgPing24h, result.AvgPing24h)
		assert.Equal(t, &uptime30d, result.Uptime30d)
		assert.Equal(t, &avgPing30d, result.AvgPing30d)
		assert.Equal(t, &uptime90d, result.Uptime90d)
		assert.Equal(t, &avgPing90d, result.AvgPing90d)
		assert.Equal(t, &lastPing, result.LastPing)
		assert.Equal(t, &expiryDays, result.CertExpiryDays)
		assert.Equal(t, &expiryDate, result.CertExpiryDate)

		mockMonitorService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
		mockTLSInfoService.AssertExpectations(t)
	})

	t.Run("monitor not found", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "nonexistent"

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(nil, errors.New("not found"))

		result, err := service.GetMonitorBadgeData(ctx, monitorID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to get monitor")
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("monitor is nil", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "monitor123"

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(nil, nil)

		result, err := service.GetMonitorBadgeData(ctx, monitorID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "monitor not found")
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("handles missing stats gracefully", func(t *testing.T) {
		service, mockMonitorService, mockHeartbeatService, mockStatsService, mockTLSInfoService, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		// Empty stats
		emptyStats := []*stats.Stat{}

		heartbeats := []*heartbeat.Model{}
		tlsInfo := &monitor_tls_info.TLSInfo{}

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatHourly).Return(emptyStats, nil).Once()
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatDaily).Return(emptyStats, nil).Times(2)
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, monitorID, 1, 0, (*bool)(nil), true).Return(heartbeats, nil)
		mockTLSInfoService.On("GetTLSInfo", ctx, monitorID).Return(tlsInfo, nil)

		result, err := service.GetMonitorBadgeData(ctx, monitorID)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, monitorID, result.ID)
		assert.Nil(t, result.Uptime24h)
		assert.Nil(t, result.AvgPing24h)
		assert.Nil(t, result.LastPing)
		assert.Nil(t, result.CertExpiryDays)

		mockMonitorService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
		mockTLSInfoService.AssertExpectations(t)
	})

	t.Run("TLS info error", func(t *testing.T) {
		service, mockMonitorService, mockHeartbeatService, mockStatsService, mockTLSInfoService, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		stats24h := []*stats.Stat{{}}
		summary := &stats.Stats{}
		heartbeats := []*heartbeat.Model{}

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatHourly).Return(stats24h, nil).Once()
		mockStatsService.On("StatPointsSummary", stats24h).Return(summary).Once()
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatDaily).Return(stats24h, nil).Twice()
		mockStatsService.On("StatPointsSummary", stats24h).Return(summary).Twice()
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, monitorID, 1, 0, (*bool)(nil), true).Return(heartbeats, nil)
		mockTLSInfoService.On("GetTLSInfo", ctx, monitorID).Return(nil, errors.New("TLS error"))

		result, err := service.GetMonitorBadgeData(ctx, monitorID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "TLS error")
		mockMonitorService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
		mockTLSInfoService.AssertExpectations(t)
	})
}

// Badge generation tests

func TestServiceImpl_GenerateStatusBadge(t *testing.T) {
	ctx := context.Background()

	t.Run("generates status badge for up monitor", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)

		result, err := service.GenerateStatusBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg") // Should contain SVG markup
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("generates status badge for down monitor", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusDown,
			Active: true,
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)

		result, err := service.GenerateStatusBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("generates status badge for inactive monitor", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: false,
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)

		result, err := service.GenerateStatusBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("custom badge options", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		options := &BadgeOptions{
			Style:       BadgeStyleFlat,
			Color:       "#custom",
			LabelColor:  "#custom",
			Label:       "Custom Status",
			LabelPrefix: "Prefix ",
			LabelSuffix: " Suffix",
			UpLabel:     "Online",
			DownLabel:   "Offline",
			UpColor:     "#00ff00",
			DownColor:   "#ff0000",
		}

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)

		result, err := service.GenerateStatusBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("monitor not found", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "nonexistent"

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(nil, errors.New("not found"))

		result, err := service.GenerateStatusBadge(ctx, monitorID, options)

		assert.Error(t, err)
		assert.Empty(t, result)
		mockMonitorService.AssertExpectations(t)
	})
}

func TestServiceImpl_GenerateUptimeBadge(t *testing.T) {
	ctx := context.Background()

	t.Run("generates uptime badge for 24h period", func(t *testing.T) {
		service, mockMonitorService, _, mockStatsService, _, _ := setupBadgeService()
		monitorID := "monitor123"
		duration := 24

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		uptime := 99.5
		stats24h := []*stats.Stat{{}}
		summary := &stats.Stats{
			Uptime: &uptime,
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatHourly).Return(stats24h, nil)
		mockStatsService.On("StatPointsSummary", stats24h).Return(summary)

		result, err := service.GenerateUptimeBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		assert.Contains(t, result, "99.5") // Should contain uptime value
		assert.Contains(t, result, "24h")  // Should contain period
		mockMonitorService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})

	t.Run("generates uptime badge for 30d period", func(t *testing.T) {
		service, mockMonitorService, _, mockStatsService, _, _ := setupBadgeService()
		monitorID := "monitor123"
		duration := 720 // 30 days

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		uptime := 98.2
		stats30d := []*stats.Stat{{}}
		summary := &stats.Stats{
			Uptime: &uptime,
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatDaily).Return(stats30d, nil)
		mockStatsService.On("StatPointsSummary", stats30d).Return(summary)

		result, err := service.GenerateUptimeBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		assert.Contains(t, result, "98.2")
		assert.Contains(t, result, "30d")
		mockMonitorService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})

	t.Run("generates uptime badge for 90d period", func(t *testing.T) {
		service, mockMonitorService, _, mockStatsService, _, _ := setupBadgeService()
		monitorID := "monitor123"
		duration := 2160 // 90 days

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		uptime := 97.8
		stats90d := []*stats.Stat{{}}
		summary := &stats.Stats{
			Uptime: &uptime,
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatDaily).Return(stats90d, nil)
		mockStatsService.On("StatPointsSummary", stats90d).Return(summary)

		result, err := service.GenerateUptimeBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		assert.Contains(t, result, "97.8")
		assert.Contains(t, result, "90d")
		mockMonitorService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})

	t.Run("handles missing uptime data", func(t *testing.T) {
		service, mockMonitorService, _, mockStatsService, _, _ := setupBadgeService()
		monitorID := "monitor123"
		duration := 24

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		emptyStats := []*stats.Stat{}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatHourly).Return(emptyStats, nil)

		result, err := service.GenerateUptimeBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		assert.Contains(t, result, "0.0") // Should default to 0.0
		mockMonitorService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})

	t.Run("custom badge options", func(t *testing.T) {
		service, mockMonitorService, _, mockStatsService, _, _ := setupBadgeService()
		monitorID := "monitor123"
		duration := 24

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		uptime := 99.5
		stats24h := []*stats.Stat{{}}
		summary := &stats.Stats{
			Uptime: &uptime,
		}

		options := &BadgeOptions{
			Style:       BadgeStyleFlat,
			Color:       "#custom",
			LabelColor:  "#custom",
			Label:       "Availability",
			LabelPrefix: "Custom ",
			LabelSuffix: " Label",
			Prefix:      "Pre",
			Suffix:      " Post",
		}

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatHourly).Return(stats24h, nil)
		mockStatsService.On("StatPointsSummary", stats24h).Return(summary)

		result, err := service.GenerateUptimeBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		mockMonitorService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})
}

func TestServiceImpl_GeneratePingBadge(t *testing.T) {
	ctx := context.Background()

	t.Run("generates ping badge for 24h period", func(t *testing.T) {
		service, mockMonitorService, _, mockStatsService, _, _ := setupBadgeService()
		monitorID := "monitor123"
		duration := 24

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		avgPing := 150.5
		stats24h := []*stats.Stat{{}}
		summary := &stats.Stats{
			AvgPing: &avgPing,
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatHourly).Return(stats24h, nil)
		mockStatsService.On("StatPointsSummary", stats24h).Return(summary)

		result, err := service.GeneratePingBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		assert.Contains(t, result, "150") // Should contain ping value (rounded)
		assert.Contains(t, result, "ms")  // Should contain default suffix
		assert.Contains(t, result, "24h") // Should contain period
		mockMonitorService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})

	t.Run("handles missing ping data", func(t *testing.T) {
		service, mockMonitorService, _, mockStatsService, _, _ := setupBadgeService()
		monitorID := "monitor123"
		duration := 24

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		emptyStats := []*stats.Stat{}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatHourly).Return(emptyStats, nil)

		result, err := service.GeneratePingBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		assert.Contains(t, result, "0") // Should default to 0
		mockMonitorService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})
}

func TestServiceImpl_GenerateCertExpBadge(t *testing.T) {
	ctx := context.Background()

	t.Run("generates cert expiry badge with valid certificate", func(t *testing.T) {
		service, mockMonitorService, _, _, mockTLSInfoService, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		expiryDays := 30
		expiryDate := time.Now().AddDate(0, 0, expiryDays)
		tlsInfo := &monitor_tls_info.TLSInfo{
			CertInfo: &shared.CertificateInfo{
				DaysRemaining: expiryDays,
				ValidTo:       expiryDate,
			},
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockTLSInfoService.On("GetTLSInfo", ctx, monitorID).Return(tlsInfo, nil)

		result, err := service.GenerateCertExpBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		assert.Contains(t, result, "30d") // Should contain days remaining
		mockMonitorService.AssertExpectations(t)
		mockTLSInfoService.AssertExpectations(t)
	})

	t.Run("generates cert expiry badge with expiring certificate", func(t *testing.T) {
		service, mockMonitorService, _, _, mockTLSInfoService, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		expiryDays := 3 // Within default down days (7)
		expiryDate := time.Now().AddDate(0, 0, expiryDays)
		tlsInfo := &monitor_tls_info.TLSInfo{
			CertInfo: &shared.CertificateInfo{
				DaysRemaining: expiryDays,
				ValidTo:       expiryDate,
			},
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockTLSInfoService.On("GetTLSInfo", ctx, monitorID).Return(tlsInfo, nil)

		result, err := service.GenerateCertExpBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		assert.Contains(t, result, "3d") // Should contain days remaining
		mockMonitorService.AssertExpectations(t)
		mockTLSInfoService.AssertExpectations(t)
	})

	t.Run("generates cert expiry badge with expired certificate", func(t *testing.T) {
		service, mockMonitorService, _, _, mockTLSInfoService, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		expiryDays := -5 // Expired
		expiryDate := time.Now().AddDate(0, 0, expiryDays)
		tlsInfo := &monitor_tls_info.TLSInfo{
			CertInfo: &shared.CertificateInfo{
				DaysRemaining: expiryDays,
				ValidTo:       expiryDate,
			},
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockTLSInfoService.On("GetTLSInfo", ctx, monitorID).Return(tlsInfo, nil)

		result, err := service.GenerateCertExpBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		assert.Contains(t, result, "Expired") // Should show expired status
		mockMonitorService.AssertExpectations(t)
		mockTLSInfoService.AssertExpectations(t)
	})

	t.Run("generates cert expiry badge with no certificate info", func(t *testing.T) {
		service, mockMonitorService, _, _, mockTLSInfoService, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		tlsInfo := &monitor_tls_info.TLSInfo{} // No cert info

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockTLSInfoService.On("GetTLSInfo", ctx, monitorID).Return(tlsInfo, nil)

		result, err := service.GenerateCertExpBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		assert.Contains(t, result, "N/A") // Should show N/A
		mockMonitorService.AssertExpectations(t)
		mockTLSInfoService.AssertExpectations(t)
	})
}

func TestServiceImpl_GenerateResponseBadge(t *testing.T) {
	ctx := context.Background()

	t.Run("generates response badge with latest ping", func(t *testing.T) {
		service, mockMonitorService, mockHeartbeatService, _, _, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		lastPing := 120
		heartbeats := []*heartbeat.Model{
			{
				ID:        "hb1",
				MonitorID: monitorID,
				Ping:      lastPing,
			},
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, monitorID, 1, 0, (*bool)(nil), true).Return(heartbeats, nil)

		result, err := service.GenerateResponseBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		assert.Contains(t, result, "120") // Should contain ping value
		assert.Contains(t, result, "ms")  // Should contain default suffix
		mockMonitorService.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
	})

	t.Run("generates response badge with no heartbeats", func(t *testing.T) {
		service, mockMonitorService, mockHeartbeatService, _, _, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		heartbeats := []*heartbeat.Model{} // No heartbeats

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, monitorID, 1, 0, (*bool)(nil), true).Return(heartbeats, nil)

		result, err := service.GenerateResponseBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		assert.Contains(t, result, "N/A") // Should show N/A
		mockMonitorService.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
	})

	t.Run("custom suffix", func(t *testing.T) {
		service, mockMonitorService, mockHeartbeatService, _, _, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		lastPing := 120
		heartbeats := []*heartbeat.Model{
			{
				ID:        "hb1",
				MonitorID: monitorID,
				Ping:      lastPing,
			},
		}

		options := &BadgeOptions{
			Style:      BadgeStyleFlat,
			Color:      DefaultBadgeOptions().Color,
			LabelColor: "#555",
			Label:      "Response Time",
			Suffix:     " milliseconds",
		}

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, monitorID, 1, 0, (*bool)(nil), true).Return(heartbeats, nil)

		result, err := service.GenerateResponseBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		assert.Contains(t, result, "120")
		mockMonitorService.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
	})
}

// Model utility method tests

func TestMonitorBadgeData_GetStatusText(t *testing.T) {
	options := DefaultBadgeOptions()

	tests := []struct {
		name     string
		data     *MonitorBadgeData
		expected string
	}{
		{
			name: "active up monitor",
			data: &MonitorBadgeData{
				Status: int(shared.MonitorStatusUp),
				Active: true,
			},
			expected: options.UpLabel,
		},
		{
			name: "active down monitor",
			data: &MonitorBadgeData{
				Status: int(shared.MonitorStatusDown),
				Active: true,
			},
			expected: options.DownLabel,
		},
		{
			name: "pending monitor",
			data: &MonitorBadgeData{
				Status: int(shared.MonitorStatusPending),
				Active: true,
			},
			expected: "Pending",
		},
		{
			name: "maintenance monitor",
			data: &MonitorBadgeData{
				Status: int(shared.MonitorStatusMaintenance),
				Active: true,
			},
			expected: "Maintenance",
		},
		{
			name: "inactive monitor",
			data: &MonitorBadgeData{
				Status: int(shared.MonitorStatusUp),
				Active: false,
			},
			expected: "Paused",
		},
		{
			name: "unknown status",
			data: &MonitorBadgeData{
				Status: 999,
				Active: true,
			},
			expected: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.data.GetStatusText(options)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMonitorBadgeData_GetStatusColor(t *testing.T) {
	options := DefaultBadgeOptions()

	tests := []struct {
		name     string
		data     *MonitorBadgeData
		expected string
	}{
		{
			name: "active up monitor",
			data: &MonitorBadgeData{
				Status: int(shared.MonitorStatusUp),
				Active: true,
			},
			expected: options.UpColor,
		},
		{
			name: "active down monitor",
			data: &MonitorBadgeData{
				Status: int(shared.MonitorStatusDown),
				Active: true,
			},
			expected: options.DownColor,
		},
		{
			name: "pending monitor",
			data: &MonitorBadgeData{
				Status: int(shared.MonitorStatusPending),
				Active: true,
			},
			expected: "#fe7d37",
		},
		{
			name: "maintenance monitor",
			data: &MonitorBadgeData{
				Status: int(shared.MonitorStatusMaintenance),
				Active: true,
			},
			expected: "#7c69ef",
		},
		{
			name: "inactive monitor",
			data: &MonitorBadgeData{
				Status: int(shared.MonitorStatusUp),
				Active: false,
			},
			expected: "#9f9f9f",
		},
		{
			name: "unknown status",
			data: &MonitorBadgeData{
				Status: 999,
				Active: true,
			},
			expected: "#9f9f9f",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.data.GetStatusColor(options)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetUptimeColor(t *testing.T) {
	tests := []struct {
		name     string
		uptime   float64
		expected string
	}{
		{
			name:     "excellent uptime (99.5%)",
			uptime:   99.5,
			expected: "#4c1",
		},
		{
			name:     "good uptime (95%)",
			uptime:   95.0,
			expected: "#4c1",
		},
		{
			name:     "light green uptime (90%)",
			uptime:   90.0,
			expected: "#97CA00",
		},
		{
			name:     "yellow-green uptime (85%)",
			uptime:   85.0,
			expected: "#a4a61d",
		},
		{
			name:     "yellow uptime (80%)",
			uptime:   80.0,
			expected: "#dfb317",
		},
		{
			name:     "orange uptime (70%)",
			uptime:   70.0,
			expected: "#fe7d37",
		},
		{
			name:     "poor uptime (50%)",
			uptime:   50.0,
			expected: "#e05d44",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetUptimeColor(tt.uptime)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetCertExpiryStatus(t *testing.T) {
	options := DefaultBadgeOptions()

	tests := []struct {
		name          string
		days          int
		expectedValue string
		expectedColor string
	}{
		{
			name:          "expired certificate",
			days:          -5,
			expectedValue: "Expired",
			expectedColor: options.DownColor,
		},
		{
			name:          "expiring within down days",
			days:          3,
			expectedValue: "3d",
			expectedColor: options.DownColor,
		},
		{
			name:          "expiring within warning days",
			days:          10,
			expectedValue: "10d",
			expectedColor: "#fe7d37",
		},
		{
			name:          "good certificate",
			days:          30,
			expectedValue: "30d",
			expectedColor: options.UpColor,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, color := GetCertExpiryStatus(tt.days, options)
			assert.Equal(t, tt.expectedValue, value)
			assert.Equal(t, tt.expectedColor, color)
		})
	}
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		prefix   string
		suffix   string
		expected string
	}{
		{
			name:     "value only",
			value:    "100",
			prefix:   "",
			suffix:   "",
			expected: "100",
		},
		{
			name:     "value with suffix",
			value:    "100",
			prefix:   "",
			suffix:   "ms",
			expected: "100ms",
		},
		{
			name:     "value with prefix",
			value:    "100",
			prefix:   "~",
			suffix:   "",
			expected: "~100",
		},
		{
			name:     "value with prefix and suffix",
			value:    "100",
			prefix:   "~",
			suffix:   "ms",
			expected: "~100ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatValue(tt.value, tt.prefix, tt.suffix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatLabel(t *testing.T) {
	tests := []struct {
		name     string
		label    string
		prefix   string
		suffix   string
		expected string
	}{
		{
			name:     "label only",
			label:    "status",
			prefix:   "",
			suffix:   "",
			expected: "status",
		},
		{
			name:     "label with suffix",
			label:    "status",
			prefix:   "",
			suffix:   " badge",
			expected: "status badge",
		},
		{
			name:     "label with prefix",
			label:    "status",
			prefix:   "monitor ",
			suffix:   "",
			expected: "monitor status",
		},
		{
			name:     "label with prefix and suffix",
			label:    "status",
			prefix:   "monitor ",
			suffix:   " badge",
			expected: "monitor status badge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatLabel(tt.label, tt.prefix, tt.suffix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "normal text",
			text:     "Hello World",
			expected: "Hello World",
		},
		{
			name:     "text with ampersand",
			text:     "A & B",
			expected: "A &amp; B",
		},
		{
			name:     "text with less than",
			text:     "A < B",
			expected: "A &lt; B",
		},
		{
			name:     "text with greater than",
			text:     "A > B",
			expected: "A &gt; B",
		},
		{
			name:     "text with quotes",
			text:     `Say "Hello"`,
			expected: "Say &quot;Hello&quot;",
		},
		{
			name:     "text with single quotes",
			text:     "Say 'Hello'",
			expected: "Say &#39;Hello&#39;",
		},
		{
			name:     "complex text",
			text:     `<script>alert("XSS & stuff")</script>`,
			expected: "&lt;script&gt;alert(&quot;XSS &amp; stuff&quot;)&lt;/script&gt;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeText(tt.text)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Additional edge cases and error scenarios

func TestServiceImpl_GenerateUptimeBadge_ErrorCases(t *testing.T) {
	ctx := context.Background()

	t.Run("monitor not found", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "nonexistent"
		duration := 24

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(nil, errors.New("not found"))

		result, err := service.GenerateUptimeBadge(ctx, monitorID, duration, options)

		assert.Error(t, err)
		assert.Empty(t, result)
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("stats service error", func(t *testing.T) {
		service, mockMonitorService, _, mockStatsService, _, _ := setupBadgeService()
		monitorID := "monitor123"
		duration := 24

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatHourly).Return([]*stats.Stat{}, errors.New("stats error"))

		result, err := service.GenerateUptimeBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err) // Should not error on stats failure, but use default values
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		mockMonitorService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})
}

func TestServiceImpl_GeneratePingBadge_ErrorCases(t *testing.T) {
	ctx := context.Background()

	t.Run("monitor not found", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "nonexistent"
		duration := 24

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(nil, errors.New("not found"))

		result, err := service.GeneratePingBadge(ctx, monitorID, duration, options)

		assert.Error(t, err)
		assert.Empty(t, result)
		mockMonitorService.AssertExpectations(t)
	})
}

func TestServiceImpl_GenerateCertExpBadge_ErrorCases(t *testing.T) {
	ctx := context.Background()

	t.Run("monitor not found", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "nonexistent"

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(nil, errors.New("not found"))

		result, err := service.GenerateCertExpBadge(ctx, monitorID, options)

		assert.Error(t, err)
		assert.Empty(t, result)
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("TLS service error", func(t *testing.T) {
		service, mockMonitorService, _, _, mockTLSInfoService, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockTLSInfoService.On("GetTLSInfo", ctx, monitorID).Return(nil, errors.New("TLS error"))

		result, err := service.GenerateCertExpBadge(ctx, monitorID, options)

		assert.Error(t, err)
		assert.Empty(t, result)
		mockMonitorService.AssertExpectations(t)
		mockTLSInfoService.AssertExpectations(t)
	})
}

func TestServiceImpl_GenerateResponseBadge_ErrorCases(t *testing.T) {
	ctx := context.Background()

	t.Run("monitor not found", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "nonexistent"

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(nil, errors.New("not found"))

		result, err := service.GenerateResponseBadge(ctx, monitorID, options)

		assert.Error(t, err)
		assert.Empty(t, result)
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("heartbeat service error", func(t *testing.T) {
		service, mockMonitorService, mockHeartbeatService, _, _, _ := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusUp,
			Active: true,
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID, "").Return(monitor, nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, monitorID, 1, 0, (*bool)(nil), true).Return([]*heartbeat.Model{}, errors.New("heartbeat error"))

		result, err := service.GenerateResponseBadge(ctx, monitorID, options)

		assert.NoError(t, err) // Should not error on heartbeat failure, but show N/A
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "svg")
		assert.Contains(t, result, "N/A")
		mockMonitorService.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
	})
}

func TestDefaultBadgeOptions(t *testing.T) {
	options := DefaultBadgeOptions()

	assert.NotNil(t, options)
	assert.Equal(t, BadgeStyleFlat, options.Style)
	assert.Equal(t, "#007ec6", options.Color)
	assert.Equal(t, "#555", options.LabelColor)
	assert.Equal(t, "Up", options.UpLabel)
	assert.Equal(t, "Down", options.DownLabel)
	assert.Equal(t, "#4c1", options.UpColor)
	assert.Equal(t, "#e05d44", options.DownColor)
	assert.Equal(t, "", options.Label)
	assert.Equal(t, "", options.Suffix)
	assert.Equal(t, 14, options.WarnDays)
	assert.Equal(t, 7, options.DownDays)
}
