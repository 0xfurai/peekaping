package badge

import (
	"context"
	"errors"
	"testing"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A window whose stats can't be read, or has only one of the two figures, leaves the other
// figures of the monitor as they are.
func TestServiceImpl_GetMonitorBadgeData_PartialFigures(t *testing.T) {
	ctx := context.Background()
	monitorID := "monitor123"
	monitor := &shared.Monitor{ID: monitorID, Name: "Test Monitor", Status: shared.MonitorStatusUp, Active: true}

	t.Run("one figure per window, a failed window, no heartbeat and no certificate info", func(t *testing.T) {
		service, mockMonitorService, mockHeartbeatService, mockStatsService, mockTLSInfoService, _ := setupBadgeService()
		uptime24h := 99.5
		avgPing30d := 180.0
		mockMonitorService.On("FindByIDAnyWorkspace", ctx, monitorID).Return(monitor, nil)
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(24*time.Hour), ago(0)).Return(&stats.Stats{Uptime: &uptime24h}, nil).Once()
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(30*24*time.Hour), ago(0)).Return(&stats.Stats{AvgPing: &avgPing30d}, nil).Once()
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(90*24*time.Hour), ago(0)).Return(nil, errors.New("stats error")).Once()
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, monitorID, 1, 0, (*bool)(nil), true).Return([]*heartbeat.Model(nil), errors.New("heartbeat error"))
		mockTLSInfoService.On("GetTLSInfo", ctx, monitorID).Return(nil, nil)

		result, err := service.GetMonitorBadgeData(ctx, monitorID)

		require.NoError(t, err)
		assert.Equal(t, &uptime24h, result.Uptime24h)
		assert.Nil(t, result.AvgPing24h)
		assert.Nil(t, result.Uptime30d)
		assert.Equal(t, &avgPing30d, result.AvgPing30d)
		assert.Nil(t, result.Uptime90d)
		assert.Nil(t, result.AvgPing90d)
		assert.Nil(t, result.LastPing)
		assert.Nil(t, result.CertExpiryDays)
		assert.Nil(t, result.CertExpiryDate)
		mockStatsService.AssertExpectations(t)
	})

	t.Run("the last window and the heartbeat come after two failed windows", func(t *testing.T) {
		service, mockMonitorService, mockHeartbeatService, mockStatsService, mockTLSInfoService, _ := setupBadgeService()
		uptime90d := 97.8
		avgPing90d := 200.0
		mockMonitorService.On("FindByIDAnyWorkspace", ctx, monitorID).Return(monitor, nil)
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(24*time.Hour), ago(0)).Return(nil, errors.New("stats error")).Once()
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(30*24*time.Hour), ago(0)).Return(nil, errors.New("stats error")).Once()
		mockStatsService.On("Summarize", ctx, monitorID, "", ago(90*24*time.Hour), ago(0)).Return(&stats.Stats{Uptime: &uptime90d, AvgPing: &avgPing90d}, nil).Once()
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, monitorID, 1, 0, (*bool)(nil), true).Return([]*heartbeat.Model{{Ping: 120}}, nil)
		mockTLSInfoService.On("GetTLSInfo", ctx, monitorID).Return(&monitor_tls_info.TLSInfo{}, nil)

		result, err := service.GetMonitorBadgeData(ctx, monitorID)

		require.NoError(t, err)
		assert.Nil(t, result.Uptime24h)
		assert.Nil(t, result.AvgPing24h)
		assert.Nil(t, result.Uptime30d)
		assert.Nil(t, result.AvgPing30d)
		assert.Equal(t, &uptime90d, result.Uptime90d)
		assert.Equal(t, &avgPing90d, result.AvgPing90d)
		if assert.NotNil(t, result.LastPing) {
			assert.Equal(t, 120, *result.LastPing)
		}
		assert.Nil(t, result.CertExpiryDays)
		mockStatsService.AssertExpectations(t)
	})
}

// A stats badge reads only the window its duration falls in, and shows the figure as missing
// when that window can't be read.
func TestServiceImpl_StatsBadges_ByWindow(t *testing.T) {
	ctx := context.Background()
	monitorID := "monitor123"
	monitor := &shared.Monitor{ID: monitorID, Name: "Test Monitor", Status: shared.MonitorStatusUp, Active: true}
	uptime := 91.5
	avgPing := 120.6

	for _, tt := range []struct {
		name                       string
		duration                   int
		window                     time.Duration
		ping, uptime               string
		pingMissing, uptimeMissing string
	}{
		{"25 hours is the 30 day window", 25, 30 * 24 * time.Hour, "ping (30d): 121ms", "uptime (30d): 91.5%", "ping (30d): N/A", "uptime (30d): N/A"},
		{"720 hours is the 30 day window", 720, 30 * 24 * time.Hour, "ping (30d): 121ms", "uptime (30d): 91.5%", "ping (30d): N/A", "uptime (30d): N/A"},
		{"721 hours is the 90 day window", 721, 90 * 24 * time.Hour, "ping (90d): 121ms", "uptime (90d): 91.5%", "ping (90d): N/A", "uptime (90d): N/A"},
		{"2160 hours is the 90 day window", 2160, 90 * 24 * time.Hour, "ping (90d): 121ms", "uptime (90d): 91.5%", "ping (90d): N/A", "uptime (90d): N/A"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service, mockMonitorService, _, mockStatsService, _, _ := setupBadgeService()
			mockMonitorService.On("FindByIDAnyWorkspace", ctx, monitorID).Return(monitor, nil)
			mockStatsService.On("Summarize", ctx, monitorID, "", ago(tt.window), ago(0)).Return(&stats.Stats{Uptime: &uptime, AvgPing: &avgPing}, nil).Twice()

			ping, err := service.GeneratePingBadge(ctx, monitorID, tt.duration, DefaultBadgeOptions())
			require.NoError(t, err)
			assertBadge(t, ping, tt.ping, "#007ec6")
			up, err := service.GenerateUptimeBadge(ctx, monitorID, tt.duration, DefaultBadgeOptions())
			require.NoError(t, err)
			assertBadge(t, up, tt.uptime, "#97CA00")
			mockStatsService.AssertExpectations(t)
		})
		t.Run(tt.name+", unreadable", func(t *testing.T) {
			service, mockMonitorService, _, mockStatsService, _, _ := setupBadgeService()
			mockMonitorService.On("FindByIDAnyWorkspace", ctx, monitorID).Return(monitor, nil)
			mockStatsService.On("Summarize", ctx, monitorID, "", ago(tt.window), ago(0)).Return(nil, errors.New("stats error")).Twice()

			ping, err := service.GeneratePingBadge(ctx, monitorID, tt.duration, DefaultBadgeOptions())
			require.NoError(t, err)
			assertBadge(t, ping, tt.pingMissing, "#007ec6")
			up, err := service.GenerateUptimeBadge(ctx, monitorID, tt.duration, DefaultBadgeOptions())
			require.NoError(t, err)
			assertBadge(t, up, tt.uptimeMissing, "#9f9f9f")
			mockStatsService.AssertExpectations(t)
		})
	}
}
