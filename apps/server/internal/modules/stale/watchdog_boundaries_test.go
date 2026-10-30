package stale

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A monitor of interval 60 and timeout 10 is stale after 180 seconds without a check, and a
// recheck of it has 20 seconds (its timeout plus 10) to finish before it is notified.
func TestWatchdog_Boundaries(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	saved := now.Add(-time.Hour)

	t.Run("a monitor checked exactly its threshold ago is not stale", func(t *testing.T) {
		f := newWatchdogFixture(t, testMonitor("a", 60, 10, saved))
		f.heartbeats.setLatest("a", now.Add(-180*time.Second))

		f.run(t)

		assert.Empty(t, f.rechecker.calls)
		count, err := f.store.Count(context.Background())
		require.NoError(t, err)
		assert.Zero(t, count)
	})

	t.Run("a monitor checked a second before its threshold ago is stale", func(t *testing.T) {
		f := newWatchdogFixture(t, testMonitor("a", 60, 10, saved))
		f.heartbeats.setLatest("a", now.Add(-181*time.Second))

		f.run(t)

		assert.Equal(t, []string{"a"}, f.rechecker.calls)
	})

	t.Run("a recheck that has had exactly its time is notified", func(t *testing.T) {
		f := newWatchdogFixture(t, testMonitor("a", 60, 10, saved))
		f.heartbeats.setLatest("a", now.Add(-4*time.Minute))
		f.run(t)

		f.at(20*time.Second - time.Nanosecond)
		f.run(t)
		assert.Empty(t, f.published(MonitorStale))

		f.at(20 * time.Second)
		f.run(t)
		assert.Len(t, f.published(MonitorStale), 1)
	})

	t.Run("a monitor whose latest check is the one it was found stale at is not recovered", func(t *testing.T) {
		since := now.Add(-time.Minute)
		f := newWatchdogFixture(t, testMonitor("a", 60, 10, saved))
		f.heartbeats.setLatest("a", since)
		require.NoError(t, f.store.Put(context.Background(), Entry{MonitorID: "a", Since: since, DetectedAt: now.Add(-time.Minute), Notified: true}))

		f.run(t)

		assert.Empty(t, f.published(MonitorStaleRecovered))
		count, err := f.store.Count(context.Background())
		require.NoError(t, err)
		assert.Equal(t, int64(1), count, "a notified record stays until a check after it")
	})

	t.Run("a monitor checked a second after it was found stale is recovered", func(t *testing.T) {
		since := now.Add(-4 * time.Minute)
		f := newWatchdogFixture(t, testMonitor("a", 60, 10, saved))
		f.heartbeats.setLatest("a", since.Add(time.Second))
		require.NoError(t, f.store.Put(context.Background(), Entry{MonitorID: "a", Since: since, DetectedAt: now.Add(-time.Minute), Notified: true}))
		f.at(-time.Minute) // the heartbeat is then 2 minutes 59 seconds old: within the threshold

		f.run(t)

		assert.Len(t, f.published(MonitorStaleRecovered), 1)
		count, err := f.store.Count(context.Background())
		require.NoError(t, err)
		assert.Zero(t, count)
	})
}
