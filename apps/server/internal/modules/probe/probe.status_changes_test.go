package probe_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/modules/probe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// at is a time on 2026-10-20, hh:mm:ss in UTC
func at(hh, mm, ss int) time.Time {
	return time.Date(2026, 10, 20, hh, mm, ss, 0, time.UTC)
}

// longAgo is before every time these tests use
var longAgo = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// described writes status changes as "status time", for comparing a history with a literal
func described(changes []*probe.StatusChange) []string {
	lines := make([]string, len(changes))
	for i, c := range changes {
		lines[i] = string(c.Status) + " " + c.At.UTC().Format("15:04:05")
	}
	return lines
}

// bareProbe makes a probe with no history, so that a test sets the times itself
func bareProbe(t *testing.T, db *bun.DB, repo probe.Repository, name string) *probe.Model {
	t.Helper()
	created, err := repo.Create(context.Background(), &probe.CreateModel{Name: name, TokenHash: "sha256$" + name, TokenPrefix: "pkp_" + name})
	require.NoError(t, err)
	_, err = db.Exec("DELETE FROM probe_status_changes WHERE probe_id = ?", created.ID)
	require.NoError(t, err)
	return created
}

func TestSQLRepository_CreateStartsTheHistoryAsOfflineAtCreation(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		before := time.Now().UTC().Add(-time.Second)

		created, err := repo.Create(ctx, &probe.CreateModel{Name: "Frankfurt", TokenHash: "sha256$aa", TokenPrefix: "pkp_aa"})
		require.NoError(t, err)

		changes, err := repo.FindStatusChanges(ctx, created.ID, longAgo)
		require.NoError(t, err)
		require.Len(t, changes, 1)
		assert.Equal(t, created.ID, changes[0].ProbeID)
		assert.Equal(t, probe.StatusOffline, changes[0].Status)
		assert.WithinDuration(t, created.CreatedAt, changes[0].At, 2*time.Second)
		assert.True(t, changes[0].At.After(before), "the probe starts offline when it is created")
	})
}

func TestSQLRepository_UpdateStatusRecordsOneChangeAtTheGivenTime(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		p := bareProbe(t, db, repo, "First")

		changed, err := repo.UpdateStatus(ctx, p.ID, probe.StatusOffline, probe.StatusOnline, at(12, 0, 5))
		require.NoError(t, err)
		require.True(t, changed)
		changed, err = repo.UpdateStatus(ctx, p.ID, probe.StatusOnline, probe.StatusOffline, at(12, 30, 0))
		require.NoError(t, err)
		require.True(t, changed)

		changes, err := repo.FindStatusChanges(ctx, p.ID, longAgo)
		require.NoError(t, err)
		assert.Equal(t, []string{"online 12:00:05", "offline 12:30:00"}, described(changes), "the time given, not the time of the call")
	})
}

func TestSQLRepository_UpdateStatusRecordsNothingWhenItChangesNothing(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		p := bareProbe(t, db, repo, "First")
		disabled := true
		other := bareProbe(t, db, repo, "Second")
		_, err := repo.Update(ctx, other.ID, &probe.UpdateModel{Disabled: &disabled})
		require.NoError(t, err)

		changed, err := repo.UpdateStatus(ctx, p.ID, probe.StatusOnline, probe.StatusOffline, at(12, 0, 0))
		require.NoError(t, err)
		assert.False(t, changed, "the probe is offline, not online")
		changed, err = repo.UpdateStatus(ctx, "no-such-probe", probe.StatusOffline, probe.StatusOnline, at(12, 0, 0))
		require.NoError(t, err)
		assert.False(t, changed)
		changed, err = repo.UpdateStatus(ctx, other.ID, probe.StatusOffline, probe.StatusOnline, at(12, 0, 0))
		require.NoError(t, err)
		assert.False(t, changed, "a disabled probe is not brought online")

		for _, id := range []string{p.ID, other.ID} {
			changes, err := repo.FindStatusChanges(ctx, id, longAgo)
			require.NoError(t, err)
			assert.Empty(t, changes)
		}
	})
}

func TestSQLRepository_UpdateStatusCalledAtOnceRecordsOneChange(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		p := bareProbe(t, db, repo, "First")

		const callers = 8
		var wg sync.WaitGroup
		results := make([]bool, callers)
		for i := range callers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				changed, err := repo.UpdateStatus(ctx, p.ID, probe.StatusOffline, probe.StatusOnline, at(12, 0, i))
				assert.NoError(t, err)
				results[i] = changed
			}()
		}
		wg.Wait()

		winners := 0
		for _, changed := range results {
			if changed {
				winners++
			}
		}
		assert.Equal(t, 1, winners, "exactly one caller changes the status")
		changes, err := repo.FindStatusChanges(ctx, p.ID, longAgo)
		require.NoError(t, err)
		require.Len(t, changes, 1)
		assert.Equal(t, probe.StatusOnline, changes[0].Status)
	})
}

func TestSQLRepository_UpdateStatusNeverRecordsATimeBeforeTheProbesLatestChange(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		p := bareProbe(t, db, repo, "First")
		_, err := repo.UpdateStatus(ctx, p.ID, probe.StatusOffline, probe.StatusOnline, at(12, 0, 0))
		require.NoError(t, err)

		// The sweep finds the instances last seen at 11:59:50, before the request that brought the
		// probe online at 12:00:00
		changed, err := repo.UpdateStatus(ctx, p.ID, probe.StatusOnline, probe.StatusOffline, at(11, 59, 50))
		require.NoError(t, err)
		require.True(t, changed)

		changes, err := repo.FindStatusChanges(ctx, p.ID, longAgo)
		require.NoError(t, err)
		assert.Equal(t, []string{"online 12:00:00", "offline 12:00:00"}, described(changes), "the periods don't run backwards, and the order of the changes holds")
	})
}

func TestSQLRepository_UpdateStatusInACallersTransactionRollsBackWithIt(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		p := bareProbe(t, db, repo, "First")
		boom := errors.New("boom")

		err := dbtx.NewSQLTransactor(db).WithTx(ctx, func(ctx context.Context) error {
			changed, err := repo.UpdateStatus(ctx, p.ID, probe.StatusOffline, probe.StatusOnline, at(12, 0, 0))
			require.NoError(t, err)
			require.True(t, changed)
			return boom
		})

		require.ErrorIs(t, err, boom)
		got, err := repo.FindByID(ctx, p.ID)
		require.NoError(t, err)
		assert.Equal(t, probe.StatusOffline, got.Status)
		changes, err := repo.FindStatusChanges(ctx, p.ID, longAgo)
		require.NoError(t, err)
		assert.Empty(t, changes, "the change and its row stand or fall together")
	})
}

func TestSQLRepository_FindStatusChangesCarriesInTheLatestChangeAtOrBeforeSince(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		p := bareProbe(t, db, repo, "First")
		other := bareProbe(t, db, repo, "Second")
		for _, step := range []struct {
			from, to probe.Status
			when     time.Time
		}{
			{probe.StatusOffline, probe.StatusOnline, at(8, 0, 0)},
			{probe.StatusOnline, probe.StatusOffline, at(10, 0, 0)},
			{probe.StatusOffline, probe.StatusOnline, at(11, 0, 0)},
			{probe.StatusOnline, probe.StatusOffline, at(13, 0, 0)},
		} {
			changed, err := repo.UpdateStatus(ctx, p.ID, step.from, step.to, step.when)
			require.NoError(t, err)
			require.True(t, changed)
		}
		_, err := repo.UpdateStatus(ctx, other.ID, probe.StatusOffline, probe.StatusOnline, at(12, 0, 0))
		require.NoError(t, err)

		between, err := repo.FindStatusChanges(ctx, p.ID, at(12, 0, 0))
		require.NoError(t, err)
		assert.Equal(t, []string{"online 11:00:00", "offline 13:00:00"}, described(between), "the status carried into the range, then what happened in it")

		exact, err := repo.FindStatusChanges(ctx, p.ID, at(10, 0, 0))
		require.NoError(t, err)
		assert.Equal(t, []string{"offline 10:00:00", "online 11:00:00", "offline 13:00:00"}, described(exact), "a change at since is the status at since")

		after, err := repo.FindStatusChanges(ctx, p.ID, at(14, 0, 0))
		require.NoError(t, err)
		assert.Equal(t, []string{"offline 13:00:00"}, described(after))

		before, err := repo.FindStatusChanges(ctx, p.ID, at(7, 0, 0))
		require.NoError(t, err)
		assert.Len(t, before, 4, "nothing to carry in before the first change")
	})
}

func TestSQLRepository_DeleteStatusChangesOlderThanKeepsEachProbesLatestBeforeTheCutoff(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		busy := bareProbe(t, db, repo, "Busy")
		quiet := bareProbe(t, db, repo, "Quiet")
		fresh := bareProbe(t, db, repo, "Fresh")
		for _, step := range []struct {
			id       string
			from, to probe.Status
			when     time.Time
		}{
			{busy.ID, probe.StatusOffline, probe.StatusOnline, at(1, 0, 0)},
			{busy.ID, probe.StatusOnline, probe.StatusOffline, at(2, 0, 0)},
			{busy.ID, probe.StatusOffline, probe.StatusOnline, at(3, 0, 0)},
			{busy.ID, probe.StatusOnline, probe.StatusOffline, at(7, 0, 0)},
			{quiet.ID, probe.StatusOffline, probe.StatusOnline, at(1, 30, 0)},
			{fresh.ID, probe.StatusOffline, probe.StatusOnline, at(6, 0, 0)},
		} {
			changed, err := repo.UpdateStatus(ctx, step.id, step.from, step.to, step.when)
			require.NoError(t, err)
			require.True(t, changed)
		}

		deleted, err := repo.DeleteStatusChangesOlderThan(ctx, at(5, 0, 0))

		require.NoError(t, err)
		assert.Equal(t, 2, deleted, "the busy probe's 01:00 and 02:00")
		read := func(p *probe.Model) []string {
			changes, err := repo.FindStatusChanges(ctx, p.ID, longAgo)
			require.NoError(t, err)
			return described(changes)
		}
		assert.Equal(t, []string{"online 03:00:00", "offline 07:00:00"}, read(busy), "its latest change before the cutoff stays, so its status at 05:00 is known")
		assert.Equal(t, []string{"online 01:30:00"}, read(quiet), "a probe whose only change is old keeps it")
		assert.Equal(t, []string{"online 06:00:00"}, read(fresh))

		deleted, err = repo.DeleteStatusChangesOlderThan(ctx, at(5, 0, 0))
		require.NoError(t, err)
		assert.Zero(t, deleted, "a second run finds nothing more")
	})
}

func TestSQLRepository_DeletingAProbeDeletesItsStatusChanges(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		p, err := repo.Create(ctx, &probe.CreateModel{Name: "Gone", TokenHash: "sha256$gone", TokenPrefix: "pkp_gone"})
		require.NoError(t, err)

		require.NoError(t, repo.Delete(ctx, p.ID))

		count, err := db.NewSelect().Table("probe_status_changes").Where("probe_id = ?", p.ID).Count(ctx)
		require.NoError(t, err)
		assert.Zero(t, count)
	})
}
