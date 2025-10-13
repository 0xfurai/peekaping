package producer

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNextAligned(t *testing.T) {
	tests := []struct {
		name     string
		after    time.Time
		period   time.Duration
		expected func(time.Time) bool
	}{
		{
			name:   "align to 60 seconds",
			after:  time.Date(2024, 1, 1, 12, 0, 30, 0, time.UTC),
			period: 60 * time.Second,
			expected: func(result time.Time) bool {
				expected := time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC)
				return result.Equal(expected)
			},
		},
		{
			name:   "align to 30 seconds from exact boundary",
			after:  time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			period: 30 * time.Second,
			expected: func(result time.Time) bool {
				expected := time.Date(2024, 1, 1, 12, 0, 30, 0, time.UTC)
				return result.Equal(expected)
			},
		},
		{
			name:   "align to 10 seconds",
			after:  time.Date(2024, 1, 1, 12, 0, 5, 500*int(time.Millisecond), time.UTC),
			period: 10 * time.Second,
			expected: func(result time.Time) bool {
				expected := time.Date(2024, 1, 1, 12, 0, 10, 0, time.UTC)
				return result.Equal(expected)
			},
		},
		{
			name:   "align to 1 second",
			after:  time.Date(2024, 1, 1, 12, 0, 0, 500*int(time.Millisecond), time.UTC),
			period: 1 * time.Second,
			expected: func(result time.Time) bool {
				expected := time.Date(2024, 1, 1, 12, 0, 1, 0, time.UTC)
				return result.Equal(expected)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nextAligned(tt.after, tt.period)
			assert.True(t, tt.expected(result), "Expected aligned time to match")
			assert.True(t, result.After(tt.after), "Result should be after input time")
			assert.Equal(t, time.UTC, result.Location(), "Result should be in UTC")
		})
	}
}

func TestToStringSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected []string
	}{
		{
			name:     "string slice",
			input:    []interface{}{"monitor1", "monitor2", "monitor3"},
			expected: []string{"monitor1", "monitor2", "monitor3"},
		},
		{
			name:     "byte slice",
			input:    []interface{}{[]byte("monitor1"), []byte("monitor2")},
			expected: []string{"monitor1", "monitor2"},
		},
		{
			name:     "mixed types",
			input:    []interface{}{"monitor1", []byte("monitor2"), 123},
			expected: []string{"monitor1", "monitor2", "123"},
		},
		{
			name:     "empty slice",
			input:    []interface{}{},
			expected: []string{},
		},
		{
			name:     "non-slice input",
			input:    "not a slice",
			expected: []string{},
		},
		{
			name:     "nil input",
			input:    nil,
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toStringSlice(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRedisNowMs(t *testing.T) {
	t.Run("successful Redis time fetch", func(t *testing.T) {
		// Create a mock Redis client
		mockClient := redis.NewClient(&redis.Options{
			Addr: "localhost:6379",
		})

		logger := zap.NewNop().Sugar()
		producer := &Producer{
			rdb:    mockClient,
			logger: logger,
			ctx:    context.Background(),
		}

		// Note: This test requires a running Redis instance
		// If Redis is not available, it should fallback to local time
		result := producer.redisNowMs()

		// Verify result is a reasonable timestamp (not too far from now)
		now := time.Now().UTC().UnixMilli()
		diff := result - now
		if diff < 0 {
			diff = -diff
		}

		// Should be within 10 seconds (allowing for clock drift and test execution time)
		assert.True(t, diff < 10000, "Redis time should be close to local time")
	})
}
