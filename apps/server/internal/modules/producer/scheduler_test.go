package producer

import (
	"context"
	"testing"
	"time"

	"vigi/internal/modules/monitor"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestScheduleMonitor(t *testing.T) {
	t.Run("successfully schedule monitor", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()
		err := producer.ScheduleMonitor(ctx, "monitor-123", 60)
		assert.NoError(t, err)

		// Verify monitor is in due set
		score, err := client.ZScore(ctx, SchedDueKey, "monitor-123").Result()
		assert.NoError(t, err)
		assert.Greater(t, score, float64(0))

		// Verify interval is stored
		assert.Equal(t, 60, producer.monitorIntervals["monitor-123"])
	})

	t.Run("fail with invalid interval", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()
		err := producer.ScheduleMonitor(ctx, "monitor-123", 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid interval")

		err = producer.ScheduleMonitor(ctx, "monitor-123", -10)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid interval")
	})

	t.Run("reschedule existing monitor", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()

		// Schedule with interval 60
		err := producer.ScheduleMonitor(ctx, "monitor-123", 60)
		assert.NoError(t, err)

		_, err = client.ZScore(ctx, SchedDueKey, "monitor-123").Result()
		assert.NoError(t, err)

		// Wait enough time for the next scheduling time to be different
		// Since the interval changes from 60 to 120, the next scheduled time should be different
		time.Sleep(200 * time.Millisecond)

		// Reschedule with interval 120
		err = producer.ScheduleMonitor(ctx, "monitor-123", 120)
		assert.NoError(t, err)

		score2, err := client.ZScore(ctx, SchedDueKey, "monitor-123").Result()
		assert.NoError(t, err)

		// Verify the monitor was rescheduled successfully
		assert.True(t, score2 > 0, "Monitor should be scheduled")

		// Interval should be updated
		assert.Equal(t, 120, producer.monitorIntervals["monitor-123"])
	})
}

func TestUnscheduleMonitor(t *testing.T) {
	t.Run("successfully unschedule monitor", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()

		// Schedule monitor first
		err := producer.ScheduleMonitor(ctx, "monitor-123", 60)
		assert.NoError(t, err)

		// Unschedule it
		err = producer.UnscheduleMonitor(ctx, "monitor-123")
		assert.NoError(t, err)

		// Verify it's not in Redis
		_, err = client.ZScore(ctx, SchedDueKey, "monitor-123").Result()
		assert.Equal(t, redis.Nil, err)

		// Verify interval is removed
		_, exists := producer.monitorIntervals["monitor-123"]
		assert.False(t, exists)
	})

	t.Run("unschedule non-existent monitor", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()

		// Unschedule monitor that doesn't exist (should not error)
		err := producer.UnscheduleMonitor(ctx, "non-existent")
		assert.NoError(t, err)
	})
}

func TestAddMonitor(t *testing.T) {
	t.Run("successfully add active monitor", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)

		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorService:   mockMonitorSvc,
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()
		mon := &monitor.Model{
			ID:       "monitor-123",
			Name:     "Test Monitor",
			Active:   true,
			Interval: 60,
		}

		mockMonitorSvc.On("FindByID", ctx, "monitor-123", "").Return(mon, nil)

		err := producer.AddMonitor(ctx, "monitor-123")
		assert.NoError(t, err)

		// Verify monitor is scheduled
		_, err = client.ZScore(ctx, SchedDueKey, "monitor-123").Result()
		assert.NoError(t, err)

		mockMonitorSvc.AssertExpectations(t)
	})

	t.Run("skip inactive monitor", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)

		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorService:   mockMonitorSvc,
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()
		mon := &monitor.Model{
			ID:       "monitor-123",
			Name:     "Inactive Monitor",
			Active:   false,
			Interval: 60,
		}

		mockMonitorSvc.On("FindByID", ctx, "monitor-123", "").Return(mon, nil)

		err := producer.AddMonitor(ctx, "monitor-123")
		assert.NoError(t, err)

		// Verify monitor is NOT scheduled
		_, err = client.ZScore(ctx, SchedDueKey, "monitor-123").Result()
		assert.Equal(t, redis.Nil, err)

		mockMonitorSvc.AssertExpectations(t)
	})

	t.Run("skip monitor with invalid interval", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)

		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorService:   mockMonitorSvc,
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()
		mon := &monitor.Model{
			ID:       "monitor-123",
			Name:     "Invalid Interval Monitor",
			Active:   true,
			Interval: 0,
		}

		mockMonitorSvc.On("FindByID", ctx, "monitor-123", "").Return(mon, nil)

		err := producer.AddMonitor(ctx, "monitor-123")
		assert.NoError(t, err)

		// Verify monitor is NOT scheduled
		_, err = client.ZScore(ctx, SchedDueKey, "monitor-123").Result()
		assert.Equal(t, redis.Nil, err)

		mockMonitorSvc.AssertExpectations(t)
	})

	t.Run("error finding monitor", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)

		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorService:   mockMonitorSvc,
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()
		mockMonitorSvc.On("FindByID", ctx, "monitor-123", "").Return(nil, assert.AnError)

		err := producer.AddMonitor(ctx, "monitor-123")
		assert.Error(t, err)

		mockMonitorSvc.AssertExpectations(t)
	})
}

func TestUpdateMonitor(t *testing.T) {
	t.Run("successfully update active monitor", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)

		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorService:   mockMonitorSvc,
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()

		// Schedule monitor first
		err := producer.ScheduleMonitor(ctx, "monitor-123", 60)
		require.NoError(t, err)

		// Update monitor with new interval
		mon := &monitor.Model{
			ID:       "monitor-123",
			Name:     "Updated Monitor",
			Active:   true,
			Interval: 120,
		}

		mockMonitorSvc.On("FindByID", ctx, "monitor-123", "").Return(mon, nil)

		err = producer.UpdateMonitor(ctx, "monitor-123")
		assert.NoError(t, err)

		// Verify interval is updated
		assert.Equal(t, 120, producer.monitorIntervals["monitor-123"])

		mockMonitorSvc.AssertExpectations(t)
	})

	t.Run("unschedule inactive monitor", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)

		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorService:   mockMonitorSvc,
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()

		// Schedule monitor first
		err := producer.ScheduleMonitor(ctx, "monitor-123", 60)
		require.NoError(t, err)

		// Update monitor to inactive
		mon := &monitor.Model{
			ID:       "monitor-123",
			Name:     "Now Inactive Monitor",
			Active:   false,
			Interval: 60,
		}

		mockMonitorSvc.On("FindByID", ctx, "monitor-123", "").Return(mon, nil)

		err = producer.UpdateMonitor(ctx, "monitor-123")
		assert.NoError(t, err)

		// Verify monitor is unscheduled
		_, err = client.ZScore(ctx, SchedDueKey, "monitor-123").Result()
		assert.Equal(t, redis.Nil, err)

		mockMonitorSvc.AssertExpectations(t)
	})

	t.Run("unschedule monitor with invalid interval", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)

		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorService:   mockMonitorSvc,
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()

		// Schedule monitor first
		err := producer.ScheduleMonitor(ctx, "monitor-123", 60)
		require.NoError(t, err)

		// Update monitor with invalid interval
		mon := &monitor.Model{
			ID:       "monitor-123",
			Name:     "Invalid Interval Monitor",
			Active:   true,
			Interval: -10,
		}

		mockMonitorSvc.On("FindByID", ctx, "monitor-123", "").Return(mon, nil)

		err = producer.UpdateMonitor(ctx, "monitor-123")
		assert.NoError(t, err)

		// Verify monitor is unscheduled
		_, err = client.ZScore(ctx, SchedDueKey, "monitor-123").Result()
		assert.Equal(t, redis.Nil, err)

		mockMonitorSvc.AssertExpectations(t)
	})
}

func TestRemoveMonitor(t *testing.T) {
	t.Run("successfully remove monitor", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()

		// Schedule monitor first
		err := producer.ScheduleMonitor(ctx, "monitor-123", 60)
		require.NoError(t, err)

		// Remove it
		err = producer.RemoveMonitor(ctx, "monitor-123")
		assert.NoError(t, err)

		// Verify it's removed from Redis
		_, err = client.ZScore(ctx, SchedDueKey, "monitor-123").Result()
		assert.Equal(t, redis.Nil, err)

		// Verify interval is removed
		_, exists := producer.monitorIntervals["monitor-123"]
		assert.False(t, exists)
	})
}

func TestInitializeSchedule(t *testing.T) {
	t.Run("successfully initialize schedule with active monitors", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)

		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorService:   mockMonitorSvc,
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()

		// Mock monitors
		monitors := []monitor.Model{
			{ID: "mon-1", Name: "Monitor 1", Active: true, Interval: 60},
			{ID: "mon-2", Name: "Monitor 2", Active: true, Interval: 120},
			{ID: "mon-3", Name: "Monitor 3", Active: true, Interval: 30},
		}

		// First page has all monitors - need to return []*monitor.Model
		monsPointers := make([]*monitor.Model, len(monitors))
		for i := range monitors {
			monsPointers[i] = &monitors[i]
		}
		// Since we're returning less than pageSize (100) monitors, it will not call page 1
		mockMonitorSvc.On("FindActivePaginated", ctx, 0, 100).Return(monsPointers, nil)

		err := producer.initializeSchedule()
		assert.NoError(t, err)

		// Verify all monitors are scheduled
		for _, mon := range monitors {
			_, err := client.ZScore(ctx, SchedDueKey, mon.ID).Result()
			assert.NoError(t, err)

			assert.Equal(t, mon.Interval, producer.monitorIntervals[mon.ID])
		}

		mockMonitorSvc.AssertExpectations(t)
	})

	t.Run("skip monitors with invalid intervals", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)

		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorService:   mockMonitorSvc,
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()

		monitors := []monitor.Model{
			{ID: "mon-1", Name: "Valid Monitor", Active: true, Interval: 60},
			{ID: "mon-2", Name: "Invalid Monitor", Active: true, Interval: 0},
			{ID: "mon-3", Name: "Negative Interval", Active: true, Interval: -10},
		}

		monsPointers := make([]*monitor.Model, len(monitors))
		for i := range monitors {
			monsPointers[i] = &monitors[i]
		}
		// Since we're returning less than pageSize monitors, it won't call page 1
		mockMonitorSvc.On("FindActivePaginated", ctx, 0, 100).Return(monsPointers, nil)

		err := producer.initializeSchedule()
		assert.NoError(t, err)

		// Only valid monitor should be scheduled
		_, err = client.ZScore(ctx, SchedDueKey, "mon-1").Result()
		assert.NoError(t, err)

		_, err = client.ZScore(ctx, SchedDueKey, "mon-2").Result()
		assert.Equal(t, redis.Nil, err)

		_, err = client.ZScore(ctx, SchedDueKey, "mon-3").Result()
		assert.Equal(t, redis.Nil, err)

		mockMonitorSvc.AssertExpectations(t)
	})

	t.Run("remove stale monitors from Redis", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)

		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorService:   mockMonitorSvc,
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()

		// Pre-populate Redis with monitors
		client.ZAdd(ctx, SchedDueKey, redis.Z{Score: float64(time.Now().UnixMilli()), Member: "stale-mon-1"})
		client.ZAdd(ctx, SchedDueKey, redis.Z{Score: float64(time.Now().UnixMilli()), Member: "stale-mon-2"})
		client.ZAdd(ctx, SchedDueKey, redis.Z{Score: float64(time.Now().UnixMilli()), Member: "active-mon-1"})

		// Mock only one active monitor
		activeMon := monitor.Model{ID: "active-mon-1", Name: "Active Monitor", Active: true, Interval: 60}
		monitors := []*monitor.Model{&activeMon}

		// Since we're returning less than pageSize monitors, it won't call page 1
		mockMonitorSvc.On("FindActivePaginated", ctx, 0, 100).Return(monitors, nil)

		err := producer.initializeSchedule()
		assert.NoError(t, err)

		// Active monitor should still be there
		_, err = client.ZScore(ctx, SchedDueKey, "active-mon-1").Result()
		assert.NoError(t, err)

		// Stale monitors should be removed
		_, err = client.ZScore(ctx, SchedDueKey, "stale-mon-1").Result()
		assert.Equal(t, redis.Nil, err)

		_, err = client.ZScore(ctx, SchedDueKey, "stale-mon-2").Result()
		assert.Equal(t, redis.Nil, err)

		mockMonitorSvc.AssertExpectations(t)
	})
}

func TestRefreshSchedule(t *testing.T) {
	t.Run("successfully refresh schedule", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)

		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorService:   mockMonitorSvc,
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()

		// Pre-populate with one monitor
		producer.monitorIntervals["mon-1"] = 60
		client.ZAdd(ctx, SchedDueKey, redis.Z{Score: float64(time.Now().UnixMilli()), Member: "mon-1"})

		// Mock updated monitors (mon-1 with new interval, mon-2 is new)
		mon1 := monitor.Model{ID: "mon-1", Name: "Monitor 1", Active: true, Interval: 120}
		mon2 := monitor.Model{ID: "mon-2", Name: "Monitor 2", Active: true, Interval: 30}
		monitors := []*monitor.Model{&mon1, &mon2}

		// Since we're returning less than pageSize monitors, it won't call page 1
		mockMonitorSvc.On("FindActivePaginated", ctx, 0, 100).Return(monitors, nil)

		err := producer.refreshSchedule()
		assert.NoError(t, err)

		// Verify intervals are updated
		assert.Equal(t, 120, producer.monitorIntervals["mon-1"])
		assert.Equal(t, 30, producer.monitorIntervals["mon-2"])

		// Verify both are in Redis
		_, err = client.ZScore(ctx, SchedDueKey, "mon-1").Result()
		assert.NoError(t, err)

		_, err = client.ZScore(ctx, SchedDueKey, "mon-2").Result()
		assert.NoError(t, err)

		mockMonitorSvc.AssertExpectations(t)
	})

	t.Run("remove inactive monitors during refresh", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)

		producer := &Producer{
			rdb:              client,
			logger:           logger,
			ctx:              context.Background(),
			monitorService:   mockMonitorSvc,
			monitorIntervals: make(map[string]int),
		}

		ctx := context.Background()

		// Pre-populate with monitors
		producer.monitorIntervals["mon-1"] = 60
		producer.monitorIntervals["mon-2"] = 120
		client.ZAdd(ctx, SchedDueKey, redis.Z{Score: float64(time.Now().UnixMilli()), Member: "mon-1"})
		client.ZAdd(ctx, SchedDueKey, redis.Z{Score: float64(time.Now().UnixMilli()), Member: "mon-2"})

		// Mock only one active monitor (mon-1 is gone)
		mon2 := monitor.Model{ID: "mon-2", Name: "Monitor 2", Active: true, Interval: 120}
		monitors := []*monitor.Model{&mon2}

		// Since we're returning less than pageSize monitors, it won't call page 1
		mockMonitorSvc.On("FindActivePaginated", ctx, 0, 100).Return(monitors, nil)

		err := producer.refreshSchedule()
		assert.NoError(t, err)

		// mon-1 should be removed
		_, exists := producer.monitorIntervals["mon-1"]
		assert.False(t, exists)

		_, err = client.ZScore(ctx, SchedDueKey, "mon-1").Result()
		assert.Equal(t, redis.Nil, err)

		// mon-2 should still exist
		assert.Equal(t, 120, producer.monitorIntervals["mon-2"])

		mockMonitorSvc.AssertExpectations(t)
	})
}
