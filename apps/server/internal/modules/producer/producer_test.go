package producer

import (
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/modules/monitor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestNewProducer(t *testing.T) {
	t.Run("create new producer with default concurrency", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		cfg := &config.Config{
			ProducerConcurrency: 0, // Not set
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
			le,
			cfg,
			logger,
		)

		assert.NotNil(t, producer)
		assert.Equal(t, ConcurrentProducers, producer.concurrency)
		assert.NotNil(t, producer.monitorIntervals)
		assert.NotNil(t, producer.ctx)
		assert.NotNil(t, producer.cancel)
	})

	t.Run("create new producer with custom concurrency", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		cfg := &config.Config{
			ProducerConcurrency: 64,
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
			le,
			cfg,
			logger,
		)

		assert.NotNil(t, producer)
		assert.Equal(t, 64, producer.concurrency)
	})
}

func TestProducerStartStop(t *testing.T) {
	t.Run("start and stop producer", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()

		logger := zap.NewNop().Sugar()
		le := NewLeaderElection(client, "node1", logger)

		cfg := &config.Config{
			ProducerConcurrency: 2, // Use small concurrency for testing
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
			le,
			cfg,
			logger,
		)

		// Mock empty monitor list for initialization
		// When returning empty list, pagination stops immediately
		mockMonitorSvc.On("FindActivePaginated", mock.Anything, 0, 100).Return([]*monitor.Model{}, nil)

		err := producer.Start()
		assert.NoError(t, err)

		// Stop immediately
		producer.Stop()
	})
}

func TestProducerContext(t *testing.T) {
	t.Run("context is cancelled on stop", func(t *testing.T) {
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
			le,
			cfg,
			logger,
		)

		// Context should not be cancelled initially
		select {
		case <-producer.ctx.Done():
			t.Fatal("Context should not be cancelled before Stop()")
		default:
			// OK
		}

		// Stop the producer
		producer.Stop()

		// Context should be cancelled after Stop()
		select {
		case <-producer.ctx.Done():
			// OK
		default:
			t.Fatal("Context should be cancelled after Stop()")
		}
	})
}

func TestProducerMonitorIntervals(t *testing.T) {
	t.Run("monitor intervals are initialized as empty map", func(t *testing.T) {
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
			le,
			cfg,
			logger,
		)

		assert.NotNil(t, producer.monitorIntervals)
		assert.Equal(t, 0, len(producer.monitorIntervals))
	})

	t.Run("monitor intervals can be accessed safely", func(t *testing.T) {
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
			le,
			cfg,
			logger,
		)

		// Add some intervals
		producer.mu.Lock()
		producer.monitorIntervals["mon-1"] = 60
		producer.monitorIntervals["mon-2"] = 120
		producer.mu.Unlock()

		// Read intervals
		producer.mu.RLock()
		assert.Equal(t, 60, producer.monitorIntervals["mon-1"])
		assert.Equal(t, 120, producer.monitorIntervals["mon-2"])
		producer.mu.RUnlock()
	})
}
