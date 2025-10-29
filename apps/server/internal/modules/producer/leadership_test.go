package producer

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/monitor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestRunLeadershipMonitor(t *testing.T) {
	t.Run("starts monitor syncing when becomes leader", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		cfg := &config.Config{
			ProducerConcurrency: 1,
		}

		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockProxySvc := new(MockProxyService)
		mockQueueSvc := new(MockQueueService)

		producer := NewProducer(
			client,
			mockQueueSvc,
			mockMonitorSvc,
			mockProxySvc,
			mockMaintenanceSvc,
			nil,
			nil,
			nil,
			le,
			cfg,
			logger,
		)

		// Mock empty monitor list
		mockMonitorSvc.On("FindActivePaginated", mock.Anything, 0, 100).Return([]monitor.Model{}, nil)

		ctx := context.Background()
		le.Start(ctx)

		// Wait for leader election
		time.Sleep(200 * time.Millisecond)

		// Start leadership monitor in a goroutine
		producer.wg.Add(1)
		go producer.runLeadershipMonitor()

		// Wait for it to detect leadership and start syncing
		time.Sleep(2 * time.Second)

		// Stop
		producer.cancel()
		producer.wg.Wait()
		le.Stop()
	})

	t.Run("stops monitor syncing when loses leadership", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		cfg := &config.Config{
			ProducerConcurrency: 1,
		}

		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockProxySvc := new(MockProxyService)
		mockQueueSvc := new(MockQueueService)

		producer := NewProducer(
			client,
			mockQueueSvc,
			mockMonitorSvc,
			mockProxySvc,
			mockMaintenanceSvc,
			nil,
			nil,
			nil,
			le,
			cfg,
			logger,
		)

		// Mock empty monitor list
		mockMonitorSvc.On("FindActivePaginated", mock.Anything, 0, 100).Return([]monitor.Model{}, nil)

		ctx := context.Background()
		le.Start(ctx)

		// Wait for leader election
		time.Sleep(200 * time.Millisecond)

		// Start leadership monitor
		producer.wg.Add(1)
		go producer.runLeadershipMonitor()

		// Wait for leadership
		time.Sleep(2 * time.Second)

		// Manually release leadership
		le.releaseLeadership(ctx)

		// Wait for it to detect loss and stop syncing
		time.Sleep(2 * time.Second)

		// Stop
		producer.cancel()
		producer.wg.Wait()
		le.Stop()
	})
}

func TestStartJobProcessing(t *testing.T) {
	t.Run("starts job processing with configured concurrency", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		cfg := &config.Config{
			ProducerConcurrency: 2,
		}

		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockProxySvc := new(MockProxyService)
		mockQueueSvc := new(MockQueueService)

		producer := NewProducer(
			client,
			mockQueueSvc,
			mockMonitorSvc,
			mockProxySvc,
			mockMaintenanceSvc,
			nil,
			nil,
			nil,
			le,
			cfg,
			logger,
		)

		err := producer.startJobProcessing()
		assert.NoError(t, err)

		// Give workers time to start
		time.Sleep(100 * time.Millisecond)

		// Stop
		producer.Stop()
	})
}

func TestStartMonitorSyncing(t *testing.T) {
	t.Run("successfully start monitor syncing", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		cfg := &config.Config{
			ProducerConcurrency: 1,
		}

		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockProxySvc := new(MockProxyService)
		mockQueueSvc := new(MockQueueService)

		producer := NewProducer(
			client,
			mockQueueSvc,
			mockMonitorSvc,
			mockProxySvc,
			mockMaintenanceSvc,
			nil,
			nil,
			nil,
			le,
			cfg,
			logger,
		)

		// Mock monitor list - need to use []*monitor.Model
		monitors := []*monitor.Model{
			{ID: "mon-1", Name: "Monitor 1", Active: true, Interval: 60},
		}
		// Mock for both initialization and the first schedule refresh (which runs every 30 seconds)
		mockMonitorSvc.On("FindActivePaginated", mock.Anything, 0, 100).Return(monitors, nil)
		mockMonitorSvc.On("FindActivePaginated", mock.Anything, 1, 100).Return([]*monitor.Model{}, nil)

		err := producer.startMonitorSyncing()
		assert.NoError(t, err)

		// Give time for schedule refresher to start
		time.Sleep(100 * time.Millisecond)

		// Stop
		producer.Stop()
	})

	t.Run("error initializing schedule", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		cfg := &config.Config{
			ProducerConcurrency: 1,
		}

		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockProxySvc := new(MockProxyService)
		mockQueueSvc := new(MockQueueService)

		producer := NewProducer(
			client,
			mockQueueSvc,
			mockMonitorSvc,
			mockProxySvc,
			mockMaintenanceSvc,
			nil,
			nil,
			nil,
			le,
			cfg,
			logger,
		)

		// Mock error
		mockMonitorSvc.On("FindActivePaginated", producer.ctx, 0, 100).Return(nil, assert.AnError)

		err := producer.startMonitorSyncing()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to initialize schedule")

		mockMonitorSvc.AssertExpectations(t)
	})
}

func TestStopMonitorSyncing(t *testing.T) {
	t.Run("stop monitor syncing", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		cfg := &config.Config{
			ProducerConcurrency: 1,
		}

		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockProxySvc := new(MockProxyService)
		mockQueueSvc := new(MockQueueService)

		producer := NewProducer(
			client,
			mockQueueSvc,
			mockMonitorSvc,
			mockProxySvc,
			mockMaintenanceSvc,
			nil,
			nil,
			nil,
			le,
			cfg,
			logger,
		)

		// Just call it - it should not panic or error
		producer.stopMonitorSyncing()
	})
}
