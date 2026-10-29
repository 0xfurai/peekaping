package monitor

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"peekaping/internal/limits"
	"peekaping/internal/modules/workspace/workspaceid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// PP-160: the lists of monitors to run leave out the active monitors past a workspace's limit. The
// workspaces over their limit are counted once for a read of many pages, not once a page.

// fixedLimits is a limits.Source with the same limits on every read
type fixedLimits limits.Snapshot

func (f fixedLimits) For(_ context.Context, id workspaceid.ID) (limits.Limits, limits.Origin, error) {
	l, origin := limits.Snapshot(f).For(id)
	return l, origin, nil
}

func (f fixedLimits) Snapshot(context.Context) (limits.Snapshot, error) {
	return limits.Snapshot(f), nil
}

// activeCounts counts the queries that count each workspace's active monitors
type activeCounts struct{ n atomic.Int32 }

func (c *activeCounts) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

func (c *activeCounts) AfterQuery(_ context.Context, event *bun.QueryEvent) {
	if strings.Contains(event.Query, "active_count") {
		c.n.Add(1)
	}
}

func TestSQLRepository_APagedReadCountsTheWorkspacesOverTheirLimitOnce(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		two := 2
		repo := NewSQLRepository(db, WithRepositoryLimits(fixedLimits{Own: map[workspaceid.ID]limits.Limits{testWorkspace: {Monitors: &two}}}))
		ctx := context.Background()
		var ids []string
		for _, name := range []string{"Oldest", "Middle", "Newest"} {
			m, err := repo.Create(ctx, testWorkspace, monitorOn(name, true, "local"))
			require.NoError(t, err)
			ids = append(ids, m.ID)
		}
		other, err := repo.Create(ctx, otherWorkspace, monitorOn("Other", true, "local"))
		require.NoError(t, err)
		counts := &activeCounts{}
		db.AddQueryHook(counts)

		pages := func(find func(page int) ([]*Model, error)) []string {
			var found []string
			for page := 0; page < 5; page++ {
				monitors, err := find(page)
				require.NoError(t, err)
				found = append(found, idsOf(monitors)...)
			}
			return found
		}
		paged := pages(func(page int) ([]*Model, error) { return repo.FindActivePaginatedAnyWorkspace(ctx, page, 1) })
		byProbe := pages(func(page int) ([]*Model, error) {
			return repo.FindActiveByProbeAnyWorkspace(ctx, "local", page, 1)
		})

		assert.ElementsMatch(t, []string{ids[0], ids[1], other.ID}, paged, "the newest past the limit is left out")
		assert.ElementsMatch(t, paged, byProbe)
		assert.EqualValues(t, 1, counts.n.Load(), "ten pages count the workspaces over their limit once")

		// Pausing one in this process counts again, and the newest takes its place
		paused := false
		require.NoError(t, repo.UpdatePartial(ctx, testWorkspace, ids[1], &UpdateModel{Active: &paused}))
		paged = pages(func(page int) ([]*Model, error) { return repo.FindActivePaginatedAnyWorkspace(ctx, page, 1) })
		assert.ElementsMatch(t, []string{ids[0], ids[2], other.ID}, paged)
		assert.EqualValues(t, 2, counts.n.Load())
	})
}

func TestSQLRepository_ChangedLimitsAreCountedAgain(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		source := &changingLimits{}
		repo := NewSQLRepository(db, WithRepositoryLimits(source))
		ctx := context.Background()
		oldest, err := repo.Create(ctx, testWorkspace, monitorOn("Oldest", true, "local"))
		require.NoError(t, err)
		newest, err := repo.Create(ctx, testWorkspace, monitorOn("Newest", true, "local"))
		require.NoError(t, err)

		one, two := 1, 2
		source.set(limits.Snapshot{Defaults: limits.Limits{Monitors: &two}})
		active, err := repo.FindActivePaginatedAnyWorkspace(ctx, 0, 10)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{oldest.ID, newest.ID}, idsOf(active))

		source.set(limits.Snapshot{Defaults: limits.Limits{Monitors: &one}})
		active, err = repo.FindActivePaginatedAnyWorkspace(ctx, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{oldest.ID}, idsOf(active), "a lowered limit applies at once")
	})
}

// changingLimits is a limits.Source whose limits a test changes
type changingLimits struct {
	snapshot atomic.Pointer[limits.Snapshot]
}

func (c *changingLimits) set(s limits.Snapshot) { c.snapshot.Store(&s) }

func (c *changingLimits) For(ctx context.Context, id workspaceid.ID) (limits.Limits, limits.Origin, error) {
	s, _ := c.Snapshot(ctx)
	l, origin := s.For(id)
	return l, origin, nil
}

func (c *changingLimits) Snapshot(context.Context) (limits.Snapshot, error) {
	if s := c.snapshot.Load(); s != nil {
		return *s, nil
	}
	return limits.Snapshot{}, nil
}
