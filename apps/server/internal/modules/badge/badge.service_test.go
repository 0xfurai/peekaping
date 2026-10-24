package badge

import (
	"context"
	"errors"
	"math"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/status_page"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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

func (m *MockMonitorService) FindByID(ctx context.Context, id string) (*shared.Monitor, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) FindByIDs(ctx context.Context, ids []string) ([]*shared.Monitor, error) {
	args := m.Called(ctx, ids)
	return args.Get(0).([]*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) FindAll(ctx context.Context, page int, limit int, q string, active *bool, status *int, tagIds []string) ([]*shared.Monitor, error) {
	args := m.Called(ctx, page, limit, q, active, status, tagIds)
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

func (m *MockMonitorService) UpdatePartial(ctx context.Context, id string, monitor *monitor.PartialUpdateDto, noPublish bool) (*shared.Monitor, error) {
	args := m.Called(ctx, id, monitor, noPublish)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockMonitorService) ValidateMonitorConfig(monitorType string, configJSON string) error {
	args := m.Called(monitorType, configJSON)
	return args.Error(0)
}

func (m *MockMonitorService) GetHeartbeats(ctx context.Context, id, probeID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, id, limit, page, important, reverse)
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

func (m *MockMonitorService) GetStatPoints(ctx context.Context, id, probeID string, since, until time.Time, granularity string) (*monitor.StatPointsSummaryDto, error) {
	args := m.Called(ctx, id, since, until, granularity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor.StatPointsSummaryDto), args.Error(1)
}

func (m *MockMonitorService) GetUptimeStats(ctx context.Context, id, probeID string) (*monitor.CustomUptimeStatsDto, error) {
	args := m.Called(ctx, id)
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

func (m *MockMonitorService) GetUptime24h(ctx context.Context, id, probeID string) (float64, error) {
	args := m.Called(ctx, id, probeID)
	return args.Get(0).(float64), args.Error(1)
}

func (m *MockMonitorService) ResetMonitorData(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockMonitorService) RepairStoredStatuses(ctx context.Context) (int, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Error(1)
}

func (m *MockMonitorService) FindActiveByProbe(ctx context.Context, probeID string, page int, limit int) ([]*shared.Monitor, error) {
	args := m.Called(ctx, probeID, page, limit)
	return args.Get(0).([]*shared.Monitor), args.Error(1)
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

func (m *MockHeartbeatService) HasAny(ctx context.Context) (bool, error) {
	args := m.Called(ctx)
	return args.Bool(0), args.Error(1)
}

func (m *MockHeartbeatService) FindByMonitorIDPaginated(ctx context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, monitorID, limit, page, important, reverse)
	return args.Get(0).([]*heartbeat.Model), args.Error(1)
}

func (m *MockHeartbeatService) FindByMonitorIDAndProbeIDPaginated(ctx context.Context, monitorID, probeID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, monitorID, probeID, limit, page, important, reverse)
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

func (m *MockStatsService) FindStatsByMonitorIDAndTimeRange(ctx context.Context, monitorID, probeID string, since, until time.Time, period stats.StatPeriod) ([]*stats.Stat, error) {
	args := m.Called(ctx, monitorID, probeID, since, until, period)
	return args.Get(0).([]*stats.Stat), args.Error(1)
}

func (m *MockStatsService) FindStatsByMonitorIDAndTimeRangeWithInterval(ctx context.Context, monitorID, probeID string, since, until time.Time, period stats.StatPeriod, monitorInterval int) ([]*stats.Stat, error) {
	args := m.Called(ctx, monitorID, probeID, since, until, period, monitorInterval)
	return args.Get(0).([]*stats.Stat), args.Error(1)
}

func (m *MockStatsService) StatPointsSummary(statsList []*stats.Stat) *stats.Stats {
	args := m.Called(statsList)
	return args.Get(0).(*stats.Stats)
}

func (m *MockStatsService) Summarize(ctx context.Context, monitorID, probeID string, since, until time.Time) (*stats.Stats, error) {
	args := m.Called(ctx, monitorID, probeID, since, until)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*stats.Stats), args.Error(1)
}

func (m *MockStatsService) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	args := m.Called(ctx, monitorID)
	return args.Error(0)
}

func (m *MockStatsService) DeleteExpired(ctx context.Context, now time.Time) (map[stats.StatPeriod]int64, error) {
	args := m.Called(ctx, now)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[stats.StatPeriod]int64), args.Error(1)
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

type MockStatusPageService struct {
	mock.Mock
	status_page.Service
}

func (m *MockStatusPageService) IsMonitorPublished(ctx context.Context, monitorID string) (bool, error) {
	args := m.Called(ctx, monitorID)
	return args.Bool(0), args.Error(1)
}

// ago matches a time the given duration before now.
func ago(d time.Duration) interface{} {
	return mock.MatchedBy(func(at time.Time) bool {
		return math.Abs(time.Since(at).Seconds()-d.Seconds()) < 60
	})
}

var (
	badgeTitle      = regexp.MustCompile(`<title>(.*?)</title>`)
	badgeValueColor = regexp.MustCompile(`<rect x="\d+" width="\d+" height="20" fill="([^"]*)"/>`)
)

// assertBadge checks what a flat badge shows, "label: value", and the color
// behind the value.
func assertBadge(t *testing.T, svg, text, color string) {
	t.Helper()
	title := badgeTitle.FindStringSubmatch(svg)
	require.NotNil(t, title, svg)
	assert.Equal(t, text, title[1])
	valueColor := badgeValueColor.FindStringSubmatch(svg)
	require.NotNil(t, valueColor, svg)
	assert.Equal(t, color, valueColor[1])
}

// Test setup helper
func setupBadgeService() (*ServiceImpl, *MockMonitorService, *MockHeartbeatService, *MockStatsService, *MockTLSInfoService, *MockStatusPageService) {
	mockMonitorService := &MockMonitorService{}
	mockHeartbeatService := &MockHeartbeatService{}
	mockStatsService := &MockStatsService{}
	mockTLSInfoService := &MockTLSInfoService{}
	mockStatusPageService := &MockStatusPageService{}
	logger := zap.NewNop().Sugar()

	service := NewService(
		mockMonitorService,
		mockHeartbeatService,
		mockStatsService,
		mockTLSInfoService,
		mockStatusPageService,
		logger,
	).(*ServiceImpl)

	return service, mockMonitorService, mockHeartbeatService, mockStatsService, mockTLSInfoService, mockStatusPageService
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
	mockStatusPageService := &MockStatusPageService{}
	logger := zap.NewNop().Sugar()

	service := NewService(
		mockMonitorService,
		mockHeartbeatService,
		mockStatsService,
		mockTLSInfoService,
		mockStatusPageService,
		logger,
	)

	assert.NotNil(t, service)
	assert.IsType(t, &ServiceImpl{}, service)

	serviceImpl := service.(*ServiceImpl)
	assert.Equal(t, mockMonitorService, serviceImpl.monitorService)
	assert.Equal(t, mockHeartbeatService, serviceImpl.heartbeatService)
	assert.Equal(t, mockStatsService, serviceImpl.statsService)
	assert.Equal(t, mockTLSInfoService, serviceImpl.tlsInfoService)
	assert.Equal(t, mockStatusPageService, serviceImpl.statusPageService)
	assert.NotNil(t, serviceImpl.svgGenerator)
	assert.NotNil(t, serviceImpl.logger)
}

// IsMonitorPublic tests

func TestServiceImpl_IsMonitorPublic(t *testing.T) {
	ctx := context.Background()
	activeMonitor := &shared.Monitor{ID: "monitor123", Name: "Test Monitor", Active: true}

	t.Run("monitor is public when on a published status page", func(t *testing.T) {
		service, mockMonitorService, _, _, _, mockStatusPageService := setupBadgeService()

		mockMonitorService.On("FindByID", ctx, "monitor123").Return(activeMonitor, nil)
		mockStatusPageService.On("IsMonitorPublished", ctx, "monitor123").Return(true, nil)

		result, err := service.IsMonitorPublic(ctx, "monitor123")

		assert.NoError(t, err)
		assert.True(t, result)
		mockMonitorService.AssertExpectations(t)
		mockStatusPageService.AssertExpectations(t)
	})

	t.Run("monitor is not public when on no published status page", func(t *testing.T) {
		service, mockMonitorService, _, _, _, mockStatusPageService := setupBadgeService()

		mockMonitorService.On("FindByID", ctx, "monitor123").Return(activeMonitor, nil)
		mockStatusPageService.On("IsMonitorPublished", ctx, "monitor123").Return(false, nil)

		result, err := service.IsMonitorPublic(ctx, "monitor123")

		assert.NoError(t, err)
		assert.False(t, result)
		mockMonitorService.AssertExpectations(t)
		mockStatusPageService.AssertExpectations(t)
	})

	t.Run("monitor not found", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "nonexistent"

		mockMonitorService.On("FindByID", ctx, monitorID).Return(nil, errors.New("not found"))

		result, err := service.IsMonitorPublic(ctx, monitorID)

		assert.Error(t, err)
		assert.False(t, result)
		assert.Contains(t, err.Error(), "not found")
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("monitor is nil", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "monitor123"

		mockMonitorService.On("FindByID", ctx, monitorID).Return(nil, nil)

		result, err := service.IsMonitorPublic(ctx, monitorID)

		assert.NoError(t, err)
		assert.False(t, result)
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("monitor is paused", func(t *testing.T) {
		service, mockMonitorService, _, _, _, mockStatusPageService := setupBadgeService()
		monitorID := "monitor123"

		monitor := &shared.Monitor{
			ID:     monitorID,
			Name:   "Test Monitor",
			Active: false,
		}

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)

		result, err := service.IsMonitorPublic(ctx, monitorID)

		assert.NoError(t, err)
		assert.False(t, result)
		mockMonitorService.AssertExpectations(t)
		mockStatusPageService.AssertNotCalled(t, "IsMonitorPublished", mock.Anything, mock.Anything)
	})

	t.Run("status page lookup fails", func(t *testing.T) {
		service, mockMonitorService, _, _, _, mockStatusPageService := setupBadgeService()

		mockMonitorService.On("FindByID", ctx, "monitor123").Return(activeMonitor, nil)
		mockStatusPageService.On("IsMonitorPublished", ctx, "monitor123").Return(false, errors.New("status page error"))

		result, err := service.IsMonitorPublic(ctx, "monitor123")

		assert.EqualError(t, err, "status page error")
		assert.False(t, result)
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(24*time.Hour), ago(0)).Return(summary24h, nil).Once()
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(30*24*time.Hour), ago(0)).Return(summary30d, nil).Once()
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(90*24*time.Hour), ago(0)).Return(summary90d, nil).Once()
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(nil, errors.New("not found"))

		result, err := service.GetMonitorBadgeData(ctx, monitorID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to get monitor")
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("monitor is nil", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "monitor123"

		mockMonitorService.On("FindByID", ctx, monitorID).Return(nil, nil)

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

		heartbeats := []*heartbeat.Model{}
		tlsInfo := &monitor_tls_info.TLSInfo{}

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockStatsService.On("Summarize", ctx, monitorID, "", mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(&stats.Stats{}, nil).Times(3)
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

		summary := &stats.Stats{}
		heartbeats := []*heartbeat.Model{}

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockStatsService.On("Summarize", ctx, monitorID, "", mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(summary, nil).Times(3)
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)

		result, err := service.GenerateStatusBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assertBadge(t, result, "status: Up", "#4c1")
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)

		result, err := service.GenerateStatusBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assertBadge(t, result, "status: Down", "#e05d44")
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)

		result, err := service.GenerateStatusBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assertBadge(t, result, "status: Paused", "#9f9f9f")
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)

		result, err := service.GenerateStatusBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assertBadge(t, result, "Prefix Custom Status Suffix: Online", "#custom")
		mockMonitorService.AssertExpectations(t)
	})

	t.Run("monitor not found", func(t *testing.T) {
		service, mockMonitorService, _, _, _, _ := setupBadgeService()
		monitorID := "nonexistent"

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID).Return(nil, errors.New("not found"))

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
		summary := &stats.Stats{
			Uptime: &uptime,
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(24*time.Hour), ago(0)).Return(summary, nil)

		result, err := service.GenerateUptimeBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err)
		assertBadge(t, result, "uptime (24h): 99.5%", "#4c1")
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
		summary := &stats.Stats{
			Uptime: &uptime,
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(30*24*time.Hour), ago(0)).Return(summary, nil)

		result, err := service.GenerateUptimeBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err)
		assertBadge(t, result, "uptime (30d): 98.2%", "#4c1")
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
		summary := &stats.Stats{
			Uptime: &uptime,
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(90*24*time.Hour), ago(0)).Return(summary, nil)

		result, err := service.GenerateUptimeBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err)
		assertBadge(t, result, "uptime (90d): 97.8%", "#4c1")
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

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(24*time.Hour), ago(0)).Return(&stats.Stats{}, nil)

		result, err := service.GenerateUptimeBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err)
		assertBadge(t, result, "uptime (24h): 0.0%", "#e05d44")
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(24*time.Hour), ago(0)).Return(summary, nil)

		result, err := service.GenerateUptimeBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err)
		assertBadge(t, result, "Custom Availability Label (24h): Pre99.5 Post", "#custom")
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
		summary := &stats.Stats{
			AvgPing: &avgPing,
		}

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(24*time.Hour), ago(0)).Return(summary, nil)

		result, err := service.GeneratePingBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err)
		assertBadge(t, result, "ping (24h): 150ms", "#007ec6")
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

		options := DefaultBadgeOptions()

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(24*time.Hour), ago(0)).Return(&stats.Stats{}, nil)

		result, err := service.GeneratePingBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err)
		assertBadge(t, result, "ping (24h): 0ms", "#007ec6")
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockTLSInfoService.On("GetTLSInfo", ctx, monitorID).Return(tlsInfo, nil)

		result, err := service.GenerateCertExpBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assertBadge(t, result, "cert exp: 30d", "#4c1")
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockTLSInfoService.On("GetTLSInfo", ctx, monitorID).Return(tlsInfo, nil)

		result, err := service.GenerateCertExpBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assertBadge(t, result, "cert exp: 3d", "#e05d44")
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockTLSInfoService.On("GetTLSInfo", ctx, monitorID).Return(tlsInfo, nil)

		result, err := service.GenerateCertExpBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assertBadge(t, result, "cert exp: Expired", "#e05d44")
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockTLSInfoService.On("GetTLSInfo", ctx, monitorID).Return(tlsInfo, nil)

		result, err := service.GenerateCertExpBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assertBadge(t, result, "cert exp: N/A", "#9f9f9f")
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, monitorID, 1, 0, (*bool)(nil), true).Return(heartbeats, nil)

		result, err := service.GenerateResponseBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assertBadge(t, result, "response: 120ms", "#007ec6")
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, monitorID, 1, 0, (*bool)(nil), true).Return(heartbeats, nil)

		result, err := service.GenerateResponseBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assertBadge(t, result, "response: N/A", "#007ec6")
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, monitorID, 1, 0, (*bool)(nil), true).Return(heartbeats, nil)

		result, err := service.GenerateResponseBadge(ctx, monitorID, options)

		assert.NoError(t, err)
		assertBadge(t, result, "Response Time: 120 milliseconds", "#007ec6")
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

func TestParseColor(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
		valid    bool
	}{
		{name: "short hex", value: "#4c1", expected: "#4c1", valid: true},
		{name: "long hex", value: "#007ec6", expected: "#007ec6", valid: true},
		{name: "short bare hex", value: "4c1", expected: "#4c1", valid: true},
		{name: "long bare hex", value: "007ec6", expected: "#007ec6", valid: true},
		{name: "hex keeps its case", value: "#ABCdef", expected: "#ABCdef", valid: true},
		{name: "bare hex keeps its case", value: "ABCdef", expected: "#ABCdef", valid: true},
		{name: "brightgreen", value: "brightgreen", expected: "#4c1", valid: true},
		{name: "green", value: "green", expected: "#97ca00", valid: true},
		{name: "yellowgreen", value: "yellowgreen", expected: "#a4a61d", valid: true},
		{name: "yellow", value: "yellow", expected: "#dfb317", valid: true},
		{name: "orange", value: "orange", expected: "#fe7d37", valid: true},
		{name: "red", value: "red", expected: "#e05d44", valid: true},
		{name: "blue", value: "blue", expected: "#007ec6", valid: true},
		{name: "grey", value: "grey", expected: "#555", valid: true},
		{name: "gray", value: "gray", expected: "#555", valid: true},
		{name: "lightgrey", value: "lightgrey", expected: "#9f9f9f", valid: true},
		{name: "lightgray", value: "lightgray", expected: "#9f9f9f", valid: true},
		{name: "success", value: "success", expected: "#4c1", valid: true},
		{name: "important", value: "important", expected: "#fe7d37", valid: true},
		{name: "critical", value: "critical", expected: "#e05d44", valid: true},
		{name: "informational", value: "informational", expected: "#007ec6", valid: true},
		{name: "inactive", value: "inactive", expected: "#9f9f9f", valid: true},
		{name: "name in any case", value: "BrightGreen", expected: "#4c1", valid: true},
		{name: "empty", value: ""},
		{name: "hash only", value: "#"},
		{name: "two digits", value: "#12"},
		{name: "four digits", value: "#1234"},
		{name: "five digits", value: "#12345"},
		{name: "seven digits", value: "#1234567"},
		{name: "eight digits", value: "#12345678"},
		{name: "not hex digits", value: "#ggg"},
		{name: "two hashes", value: "##4c1"},
		{name: "unknown name", value: "teal"},
		{name: "css function", value: "rgb(0,0,0)"},
		{name: "url", value: "url(#x)"},
		{name: "surrounding space", value: " 4c1 "},
		{name: "hex then markup", value: `#4c1"/><script>alert(1)</script>`},
		{name: "markup", value: `"><script>alert(1)</script>`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, valid := parseColor(tt.value)
			assert.Equal(t, tt.valid, valid)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSVGBadgeGenerator_EscapesColors(t *testing.T) {
	const hostile = `"><script>alert(1)</script>`
	const escaped = "&quot;&gt;&lt;script&gt;alert(1)&lt;/script&gt;"

	for _, style := range []BadgeStyle{BadgeStyleFlat, BadgeStyleFlatSquare, BadgeStylePlastic, BadgeStyleForTheBadge, BadgeStyleSocial} {
		t.Run(string(style), func(t *testing.T) {
			svg := NewSVGBadgeGenerator().GenerateBadge(&Badge{
				Style: style, Label: "status", Value: "Up", Color: hostile, LabelColor: hostile,
			})

			assert.NotContains(t, svg, "<script")
			// The label's color and the value's color
			assert.Equal(t, 2, strings.Count(svg, `fill="`+escaped+`"`), svg)
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(nil, errors.New("not found"))

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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(24*time.Hour), ago(0)).Return(nil, errors.New("stats error"))

		result, err := service.GenerateUptimeBadge(ctx, monitorID, duration, options)

		assert.NoError(t, err) // Should not error on stats failure, but use default values
		assertBadge(t, result, "uptime (24h): 0.0%", "#e05d44")
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(nil, errors.New("not found"))

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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(nil, errors.New("not found"))

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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(nil, errors.New("not found"))

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

		mockMonitorService.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, monitorID, 1, 0, (*bool)(nil), true).Return([]*heartbeat.Model{}, errors.New("heartbeat error"))

		result, err := service.GenerateResponseBadge(ctx, monitorID, options)

		assert.NoError(t, err) // Should not error on heartbeat failure, but show N/A
		assertBadge(t, result, "response: N/A", "#007ec6")
		mockMonitorService.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
	})
}

func TestDefaultBadgeOptions(t *testing.T) {
	options := DefaultBadgeOptions()

	assert.NotNil(t, options)
	assert.Equal(t, BadgeStyleFlat, options.Style)
	assert.Equal(t, "", options.Color, "no color is given until the request names one")
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
