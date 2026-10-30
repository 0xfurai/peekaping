package workspace_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// PP-170: a workspace whose owner scheduled its deletion is held until the date, and listed for the
// cleanup job once the date has come.

func TestRepository_ScheduleDeletionKeepsTheFirstDateAndTheOtherHolds(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		created, err := repo.CreateSuspended(ctx, "Waiting", workspace.SuspendedUnverified)
		require.NoError(t, err)
		due := time.Now().UTC().Add(workspace.DeletionGracePeriod).Truncate(time.Millisecond)

		scheduled, changed, err := repo.ScheduleDeletion(ctx, created.ID, due)
		require.NoError(t, err)
		again, changedAgain, err := repo.ScheduleDeletion(ctx, created.ID, due.Add(time.Hour))
		require.NoError(t, err)

		assert.True(t, changed)
		require.NotNil(t, scheduled.DeletionScheduledAt)
		assert.True(t, due.Equal(*scheduled.DeletionScheduledAt), "%s vs %s", due, scheduled.DeletionScheduledAt)
		assert.False(t, changedAgain, "a deletion that is scheduled keeps its date")
		assert.True(t, due.Equal(*again.DeletionScheduledAt))
		assert.Equal(t, workspace.SuspendedDeleting, again.ShownReason(), "the deletion is told before the verification hold")
		assert.NotNil(t, again.SuspendedAt, "the verification hold stays")
		missing, _, err := repo.ScheduleDeletion(ctx, "00000000-0000-4000-8000-000000000000", due)
		require.NoError(t, err)
		assert.Nil(t, missing)
	})
}

func TestRepository_CancelDeletionLiftsOnlyItsOwnHold(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		running, err := repo.Create(ctx, "Running")
		require.NoError(t, err)
		waiting, err := repo.CreateSuspended(ctx, "Waiting", workspace.SuspendedUnverified)
		require.NoError(t, err)
		due := time.Now().UTC().Add(workspace.DeletionGracePeriod)
		for _, id := range []workspace.ID{running.ID, waiting.ID} {
			_, _, err := repo.ScheduleDeletion(ctx, id, due)
			require.NoError(t, err)
		}

		cancelled, remaining, err := repo.CancelDeletion(ctx, running.ID)
		require.NoError(t, err)
		assert.True(t, cancelled)
		assert.False(t, remaining, "nothing else holds it")
		cancelled, remaining, err = repo.CancelDeletion(ctx, waiting.ID)
		require.NoError(t, err)
		assert.True(t, cancelled)
		assert.True(t, remaining, "it still waits for verification")
		cancelled, _, err = repo.CancelDeletion(ctx, running.ID)
		require.NoError(t, err)
		assert.False(t, cancelled, "nothing is scheduled any more")

		found, err := repo.FindByID(ctx, waiting.ID)
		require.NoError(t, err)
		assert.Nil(t, found.DeletionScheduledAt)
		assert.Equal(t, workspace.SuspendedUnverified, found.ShownReason())
	})
}

func TestRepository_ListDeletionDueAnyWorkspaceListsTheWorkspacesWhoseDateHasCome(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		names := map[string]time.Duration{"Later": time.Hour, "Exactly now": 0, "Long ago": -48 * time.Hour}
		ids := map[string]workspace.ID{}
		for name, offset := range names {
			created, err := repo.Create(ctx, name)
			require.NoError(t, err)
			_, _, err = repo.ScheduleDeletion(ctx, created.ID, now.Add(offset))
			require.NoError(t, err)
			ids[name] = created.ID
		}
		_, err := repo.Create(ctx, "Not scheduled")
		require.NoError(t, err)

		due, err := repo.ListDeletionDueAnyWorkspace(ctx, now)

		require.NoError(t, err)
		assert.Equal(t, []workspace.ID{ids["Long ago"], ids["Exactly now"]}, due, "the one due first at the front")
	})
}

func TestRepository_DeleteDueDeletesOnlyAWorkspaceWhoseDateHasCome(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		due, err := repo.Create(ctx, "Due")
		require.NoError(t, err)
		_, _, err = repo.ScheduleDeletion(ctx, due.ID, now)
		require.NoError(t, err)
		later, err := repo.Create(ctx, "Later")
		require.NoError(t, err)
		_, _, err = repo.ScheduleDeletion(ctx, later.ID, now.Add(time.Hour))
		require.NoError(t, err)
		cancelled, err := repo.Create(ctx, "Cancelled")
		require.NoError(t, err)
		_, _, err = repo.ScheduleDeletion(ctx, cancelled.ID, now)
		require.NoError(t, err)
		_, _, err = repo.CancelDeletion(ctx, cancelled.ID)
		require.NoError(t, err)

		for _, kept := range []workspace.ID{later.ID, cancelled.ID} {
			deleted, err := repo.DeleteDue(ctx, kept, now)
			require.NoError(t, err)
			assert.Nil(t, deleted)
			found, err := repo.FindByID(ctx, kept)
			require.NoError(t, err)
			assert.NotNil(t, found, "it is not due")
		}
		deleted, err := repo.DeleteDue(ctx, due.ID, now)
		require.NoError(t, err)
		assert.NotNil(t, deleted)
		found, err := repo.FindByID(ctx, due.ID)
		require.NoError(t, err)
		assert.Nil(t, found)
	})
}
