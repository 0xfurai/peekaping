package executor

import (
	"context"
	"testing"
	"time"

	"peekaping/src/modules/shared"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestPingExecutor_EnhancedTimeout(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewPingExecutor(logger)

	t.Run("MultiPingWithPerRequestTimeout", func(t *testing.T) {
		// Test with multiple pings and per-request timeout
		config := `{"host": "8.8.8.8", "packet_size": 32, "count": 3, "per_request_timeout": 2}`
		monitor := &Monitor{
			Name:    "test-ping",
			Timeout: 10, // Global timeout: 10s (should be >= 3 * 2 = 6s)
			Config:  config,
		}

		ctx := context.Background()
		result := executor.Execute(ctx, monitor, nil)

		// Should return up result for a valid host (if network allows)
		assert.NotNil(t, result)
		if result.Status == shared.MonitorStatusUp {
			assert.Contains(t, result.Message, "successful")
		}
	})

	t.Run("GlobalTimeoutValidation", func(t *testing.T) {
		// Test that global timeout validation works
		config := `{"host": "8.8.8.8", "packet_size": 32, "count": 3, "per_request_timeout": 5}`
		monitor := &Monitor{
			Name:    "test-ping",
			Timeout: 10, // Global timeout: 10s (should be >= 3 * 5 = 15s, but it's not)
			Config:  config,
		}

		ctx := context.Background()
		result := executor.Execute(ctx, monitor, nil)

		// Should return down result due to timeout validation failure
		assert.NotNil(t, result)
		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "global timeout")
		assert.Contains(t, result.Message, "theoretical max time")
	})

	t.Run("ContextTimeout", func(t *testing.T) {
		// Test with a very short timeout to verify context cancellation works
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Use a non-existent host that will timeout
		config := `{"host": "192.168.254.254", "packet_size": 32, "count": 1, "per_request_timeout": 1}`
		monitor := &Monitor{
			Name:    "test-ping",
			Timeout: 1, // 1 second timeout
			Config:  config,
		}

		result := executor.Execute(ctx, monitor, nil)

		// Should return a down result due to timeout
		assert.NotNil(t, result)
		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "ping")
	})

	t.Run("DefaultValues", func(t *testing.T) {
		// Test that default values are applied correctly
		config := `{"host": "8.8.8.8"}` // Only host specified, other fields should get defaults
		monitor := &Monitor{
			Name:    "test-ping",
			Timeout: 5, // 5 second timeout (should be >= 1 * 2 = 2s)
			Config:  config,
		}

		ctx := context.Background()
		result := executor.Execute(ctx, monitor, nil)

		// Should work with defaults: count=1, per_request_timeout=2
		assert.NotNil(t, result)
		if result.Status == shared.MonitorStatusUp {
			assert.Contains(t, result.Message, "successful")
		}
	})
}

func TestPingExecutor_Validation(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewPingExecutor(logger)

	t.Run("ValidConfig", func(t *testing.T) {
		config := `{"host": "8.8.8.8", "packet_size": 32, "count": 3, "per_request_timeout": 2}`
		err := executor.Validate(config)
		assert.NoError(t, err)
	})

	t.Run("InvalidCount", func(t *testing.T) {
		config := `{"host": "8.8.8.8", "count": 101}` // Exceeds max count
		err := executor.Validate(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Count")
		assert.Contains(t, err.Error(), "max")
	})

	t.Run("InvalidPerRequestTimeout", func(t *testing.T) {
		config := `{"host": "8.8.8.8", "per_request_timeout": 61}` // Exceeds max timeout
		err := executor.Validate(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "PerRequestTimeout")
		assert.Contains(t, err.Error(), "max")
	})

	t.Run("MissingHost", func(t *testing.T) {
		config := `{"packet_size": 32}`
		err := executor.Validate(config)
		assert.Error(t, err)
	})
}

func TestPingExecutor_ContextCancellation(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewPingExecutor(logger)

	// Test with immediate cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	config := `{"host": "8.8.8.8", "packet_size": 32}`
	monitor := &Monitor{
		Name:    "test-ping",
		Timeout: 30, // 30 second timeout
		Config:  config,
	}

	result := executor.Execute(ctx, monitor, nil)

	// Should return a down result due to context cancellation
	assert.NotNil(t, result)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "ping")
}

// TestPingExecutor_GitHubIssue157 reproduces the exact scenario from GitHub Issue #157
// https://github.com/0xfurai/peekaping/issues/157
func TestPingExecutor_GitHubIssue157(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewPingExecutor(logger)

	t.Run("DeviceOfflineScenario", func(t *testing.T) {
		// Reproduce the exact scenario from GitHub Issue #157:
		// "Adding a Ping monitor with default 20 seconds and 1 retry doesn't report as down
		// when the device goes offline (turned off or Wifi off) in a timely matter."

		// Use default configuration (count=1, per_request_timeout=2s)
		config := `{"host": "192.168.254.254"}` // Non-existent IP that will timeout
		monitor := &Monitor{
			Name:          "test-ping-issue157",
			Timeout:       20, // 20 seconds global timeout (as mentioned in issue)
			MaxRetries:    1,  // 1 retry (as mentioned in issue)
			RetryInterval: 20, // 20 seconds retry interval
			Config:        config,
		}

		// Test with context timeout that should be respected
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()

		start := time.Now()
		result := executor.Execute(ctx, monitor, nil)
		duration := time.Since(start)

		// Verify the fix: should detect as down within reasonable time
		assert.NotNil(t, result)
		assert.Equal(t, shared.MonitorStatusDown, result.Status)

		// Should complete within the global timeout (20s) + small buffer
		// Before the fix, this would hang indefinitely
		assert.Less(t, duration, 25*time.Second, "Ping should complete within global timeout")

		// Should contain appropriate error message
		assert.Contains(t, result.Message, "ping")
	})

	t.Run("DeviceOfflineWithMultiplePings", func(t *testing.T) {
		// Test the enhanced scenario with multiple pings
		config := `{"host": "192.168.254.254", "count": 3, "per_request_timeout": 2}`
		monitor := &Monitor{
			Name:          "test-ping-issue157-enhanced",
			Timeout:       20, // 20 seconds global timeout
			MaxRetries:    1,  // 1 retry
			RetryInterval: 20,
			Config:        config,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()

		start := time.Now()
		result := executor.Execute(ctx, monitor, nil)
		duration := time.Since(start)

		// Should detect as down and complete within timeout
		assert.NotNil(t, result)
		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Less(t, duration, 25*time.Second, "Multi-ping should complete within global timeout")
		assert.Contains(t, result.Message, "ping")
	})

	t.Run("DeviceOfflineFastTimeout", func(t *testing.T) {
		// Test with very short timeout to verify immediate detection
		config := `{"host": "192.168.254.254", "count": 1, "per_request_timeout": 1}`
		monitor := &Monitor{
			Name:          "test-ping-issue157-fast",
			Timeout:       5, // 5 seconds global timeout
			MaxRetries:    1,
			RetryInterval: 5,
			Config:        config,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		start := time.Now()
		result := executor.Execute(ctx, monitor, nil)
		duration := time.Since(start)

		// Should detect as down quickly
		assert.NotNil(t, result)
		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Less(t, duration, (5+2)*time.Second, "Fast timeout should complete quickly")

		// Should be much faster than the old hanging behavior (was infinite)
		// Allow some buffer for DNS resolution and network operations
		assert.Less(t, duration, (5+2)*time.Second, "Should complete within reasonable time")
	})

	t.Run("DeviceOfflineContextCancellation", func(t *testing.T) {
		// Test that context cancellation works properly (the core fix)
		config := `{"host": "192.168.254.254"}`
		monitor := &Monitor{
			Name:          "test-ping-issue157-context",
			Timeout:       20, // 20 seconds global timeout
			MaxRetries:    1,
			RetryInterval: 20,
			Config:        config,
		}

		// Cancel context after 2 seconds
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		start := time.Now()
		result := executor.Execute(ctx, monitor, nil)
		duration := time.Since(start)

		// Should respect context cancellation and complete quickly
		assert.NotNil(t, result)
		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Less(t, duration, 3*time.Second, "Should respect context cancellation")
		assert.Contains(t, result.Message, "ping")
	})
}
