package stale

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The stores are tested together: the in-memory store has to behave as the Redis one does.
var stores = map[string]func(t *testing.T) Store{
	"RedisStore": func(t *testing.T) Store {
		rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
		t.Cleanup(func() { _ = rdb.Close() })
		return NewRedisStore(rdb)
	},
	"MemoryStore": func(*testing.T) Store { return NewMemoryStore() },
}

func forEachStore(t *testing.T, test func(t *testing.T, store Store)) {
	for name, newStore := range stores {
		t.Run(name, func(t *testing.T) {
			test(t, newStore(t))
		})
	}
}

func entry(monitorID string, since time.Time, notified bool) Entry {
	return Entry{MonitorID: monitorID, Since: since, DetectedAt: since.Add(time.Minute), Notified: notified}
}

// requireEntries checks the stored entries by value; the times only have to be the same instants
func requireEntries(t *testing.T, store Store, want ...Entry) {
	t.Helper()
	got, err := store.All(context.Background())
	require.NoError(t, err)
	require.Len(t, got, len(want))
	for _, w := range want {
		g, ok := got[w.MonitorID]
		require.True(t, ok, "monitor %s is stored", w.MonitorID)
		assert.Equal(t, w.MonitorID, g.MonitorID)
		assert.True(t, w.Since.Equal(g.Since), "since %v, want %v", g.Since, w.Since)
		assert.True(t, w.DetectedAt.Equal(g.DetectedAt), "detected at %v, want %v", g.DetectedAt, w.DetectedAt)
		assert.Equal(t, w.Notified, g.Notified)
	}

	count, err := store.Count(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, len(want), count)
}

func TestStore_StartsEmpty(t *testing.T) {
	forEachStore(t, func(t *testing.T, store Store) {
		all, err := store.All(context.Background())
		require.NoError(t, err)
		assert.NotNil(t, all)
		assert.Empty(t, all)

		count, err := store.Count(context.Background())
		require.NoError(t, err)
		assert.Zero(t, count)

		ranAt, err := store.RanAt(context.Background())
		require.NoError(t, err)
		assert.Nil(t, ranAt)
	})
}

func TestStore_PutRecordsAStaleMonitorByItsID(t *testing.T) {
	forEachStore(t, func(t *testing.T, store Store) {
		since := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

		require.NoError(t, store.Put(context.Background(), entry("mon-a", since, false)))
		require.NoError(t, store.Put(context.Background(), entry("mon-b", since.Add(time.Hour), true)))

		requireEntries(t, store, entry("mon-a", since, false), entry("mon-b", since.Add(time.Hour), true))
	})
}

func TestStore_PutReplacesTheMonitorsEarlierRecord(t *testing.T) {
	forEachStore(t, func(t *testing.T, store Store) {
		since := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
		require.NoError(t, store.Put(context.Background(), entry("mon-a", since, false)))

		require.NoError(t, store.Put(context.Background(), entry("mon-a", since, true)))

		requireEntries(t, store, entry("mon-a", since, true))
	})
}

func TestStore_DeleteForgetsOnlyTheGivenMonitors(t *testing.T) {
	forEachStore(t, func(t *testing.T, store Store) {
		since := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
		for _, id := range []string{"mon-a", "mon-b", "mon-c"} {
			require.NoError(t, store.Put(context.Background(), entry(id, since, false)))
		}

		require.NoError(t, store.Delete(context.Background(), "mon-a", "mon-c"))

		requireEntries(t, store, entry("mon-b", since, false))
	})
}

func TestStore_DeleteIgnoresMonitorsThatAreNotStored(t *testing.T) {
	forEachStore(t, func(t *testing.T, store Store) {
		since := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
		require.NoError(t, store.Put(context.Background(), entry("mon-a", since, false)))

		require.NoError(t, store.Delete(context.Background()))
		require.NoError(t, store.Delete(context.Background(), "unknown"))

		requireEntries(t, store, entry("mon-a", since, false))
	})
}

func TestStore_AllReturnsACopy(t *testing.T) {
	forEachStore(t, func(t *testing.T, store Store) {
		since := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
		require.NoError(t, store.Put(context.Background(), entry("mon-a", since, false)))

		all, err := store.All(context.Background())
		require.NoError(t, err)
		delete(all, "mon-a")
		all["mon-b"] = entry("mon-b", since, false)

		requireEntries(t, store, entry("mon-a", since, false))
	})
}

func TestStore_RanAtIsTheLastTimeTheWatchdogRanInUTCToTheMillisecond(t *testing.T) {
	forEachStore(t, func(t *testing.T, store Store) {
		first := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
		second := time.Date(2026, 10, 18, 13, 30, 15, 123_456_789, time.FixedZone("UTC+2", 2*60*60))

		require.NoError(t, store.SetRanAt(context.Background(), first))
		got, err := store.RanAt(context.Background())
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, first, *got)

		require.NoError(t, store.SetRanAt(context.Background(), second))
		got, err = store.RanAt(context.Background())
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, time.Date(2026, 10, 18, 11, 30, 15, 123_000_000, time.UTC), *got)
	})
}

func TestMemoryStore_IsSafeForConcurrentUse(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	since := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	for i := range 8 {
		id := "mon-" + strconv.Itoa(i)
		wg.Go(func() {
			for range 200 {
				assert.NoError(t, store.Put(ctx, entry(id, since, false)))
				_, err := store.All(ctx)
				assert.NoError(t, err)
				_, err = store.Count(ctx)
				assert.NoError(t, err)
				assert.NoError(t, store.SetRanAt(ctx, since))
				_, err = store.RanAt(ctx)
				assert.NoError(t, err)
				assert.NoError(t, store.Delete(ctx, id))
			}
		})
	}
	wg.Wait()

	requireEntries(t, store)
}
