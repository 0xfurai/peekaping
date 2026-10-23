package producer

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/check"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/shared"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProducer_MonitorMovedToRemoteIsUnscheduledAndItsCheckCancelled(t *testing.T) {
	ctx := context.Background()
	slot := time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC)
	local := &monitor.Model{ID: "mon-1", Type: "http", Active: true, Interval: 60}
	remote := &monitor.Model{ID: "mon-1", Type: "http", Active: true, Interval: 60, ProbeIDs: []string{"probe-1"}}

	t.Run("a queued check is removed with the monitor's schedule", func(t *testing.T) {
		e, bus := newListener(t, true, local)
		e.producer.monitorIntervals["mon-1"] = 60
		e.addDue(t, "mon-1", slot)
		e.addLease(t, "mon-1", t0.Add(LeaseTTL))
		enqueued, err := e.producer.runner.Run(ctx, &check.Job{Monitor: local}, t0)
		require.NoError(t, err)
		require.True(t, enqueued)
		e.queuedCheck(t, "mon-1")
		e.monitors.put(remote)

		require.NoError(t, events.Publish(ctx, bus, events.MonitorUpdated, *remote))

		assert.Empty(t, e.members(t, SchedDueKey))
		assert.Empty(t, e.members(t, SchedLeaseKey))
		assert.NotContains(t, e.producer.monitorIntervals, "mon-1")
		e.noQueuedCheck(t, "mon-1")
	})

	t.Run("a running check is cancelled", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		mockMonitorSvc := new(MockMonitorService)
		mockQueueSvc := new(MockQueueService)
		producer := &Producer{
			rdb:              client,
			logger:           zap.NewNop().Sugar(),
			ctx:              ctx,
			monitorService:   mockMonitorSvc,
			runner:           NewQueueRunner(mockQueueSvc, zap.NewNop().Sugar()),
			monitorIntervals: make(map[string]int),
		}
		require.NoError(t, producer.ScheduleMonitor(ctx, "mon-1", 60))
		mockMonitorSvc.On("FindByID", ctx, "mon-1").Return(remote, nil)
		mockQueueSvc.On("GetTaskInfo", ctx, "healthcheck", "healthcheck:mon-1").Return(&queue.TaskInfo{State: "active"}, nil)
		mockQueueSvc.On("CancelTask", ctx, "healthcheck:mon-1").Return(nil)

		require.NoError(t, producer.UpdateMonitor(ctx, "mon-1"))

		_, err := client.ZScore(ctx, SchedDueKey, "mon-1").Result()
		assert.Equal(t, redis.Nil, err)
		mockQueueSvc.AssertExpectations(t)
		mockQueueSvc.AssertNotCalled(t, "DeleteTask", mock.Anything, mock.Anything, mock.Anything)
	})
}

func TestProducer_MovedBackIsCheckedAtOnce(t *testing.T) {
	ctx := context.Background()
	remote := &monitor.Model{ID: "mon-1", Type: "http", Active: true, Interval: 60, ProbeIDs: []string{"probe-1"}}
	local := &monitor.Model{ID: "mon-1", Type: "http", Active: true, Interval: 60, ProbeIDs: []string{shared.LocalProbeID}}
	e, bus := newListener(t, true, remote)
	require.NoError(t, events.Publish(ctx, bus, events.MonitorCreated, *remote))
	require.Empty(t, e.members(t, SchedDueKey), "a monitor on a remote probe isn't scheduled")
	e.monitors.put(local)

	require.NoError(t, events.Publish(ctx, bus, events.MonitorUpdated, *local))

	assert.Equal(t, map[string]float64{"mon-1": ms(t0)}, e.members(t, SchedDueKey))
	assert.Equal(t, 60, e.producer.monitorIntervals["mon-1"])
}

func TestProducer_RefreshSkipsRemote(t *testing.T) {
	slot := time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC)
	for name, sync := range map[string]func(*Producer) error{
		"startup": (*Producer).initializeSchedule,
		"refresh": (*Producer).refreshSchedule,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEngine(t,
				&monitor.Model{ID: "mon-local", Active: true, Interval: 60},
				&monitor.Model{ID: "mon-remote", Active: true, Interval: 60, ProbeIDs: []string{"probe-1"}},
				&monitor.Model{ID: "mon-moved", Active: true, Interval: 60, ProbeIDs: []string{"probe-1"}},
			)
			// mon-moved was scheduled before it moved to a remote probe, without an event
			e.producer.monitorIntervals["mon-moved"] = 60
			e.addDue(t, "mon-moved", slot)
			e.addLease(t, "mon-moved", t0.Add(LeaseTTL))

			require.NoError(t, sync(e.producer))

			assert.Equal(t, map[string]float64{"mon-local": ms(t0)}, e.members(t, SchedDueKey))
			assert.Empty(t, e.members(t, SchedLeaseKey))
			assert.Equal(t, map[string]int{"mon-local": 60}, e.producer.monitorIntervals)
		})
	}
}

func TestProducer_RunNowRefusesRemote(t *testing.T) {
	e := newEngine(t, &monitor.Model{ID: "mon-1", Active: true, Interval: 60, ProbeIDs: []string{"probe-1"}})

	assert.Error(t, e.producer.RunNow(context.Background(), "mon-1"))

	assert.Empty(t, e.members(t, SchedDueKey))
}
