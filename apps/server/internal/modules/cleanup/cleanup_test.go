package cleanup

import (
	"context"
	"errors"
	"testing"
	"time"

	"peekaping/internal/modules/stats"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// expiringStats is a stats service whose DeleteExpired returns fixed results.
type expiringStats struct {
	stats.Service
	deleted map[stats.StatPeriod]int64
	err     error
}

func (s expiringStats) DeleteExpired(context.Context, time.Time) (map[stats.StatPeriod]int64, error) {
	return s.deleted, s.err
}

func TestCleanupStatsLogsRowsDeletedPerPeriod(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	deleted := map[stats.StatPeriod]int64{stats.StatMinutely: 6000, stats.StatHourly: 100}

	cleanupStats(expiringStats{deleted: deleted}, zap.New(core).Sugar())

	entries := logs.FilterMessage("Deleted expired stats").All()
	require.Len(t, entries, 1)
	assert.Equal(t, deleted, entries[0].ContextMap()["count"])
}

func TestCleanupStatsLogsAFailure(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	deleted := map[stats.StatPeriod]int64{stats.StatMinutely: 5000}

	cleanupStats(expiringStats{deleted: deleted, err: errors.New("database is locked")}, zap.New(core).Sugar())

	entries := logs.FilterMessage("Failed to delete expired stats").All()
	require.Len(t, entries, 1)
	assert.Equal(t, zap.ErrorLevel, entries[0].Level)
	assert.Equal(t, "database is locked", entries[0].ContextMap()["error"])
	assert.Equal(t, deleted, entries[0].ContextMap()["count"])
	assert.Empty(t, logs.FilterMessage("Deleted expired stats").All())
}
