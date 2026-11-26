package probe_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/probe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestSQLRepository_MakeRoomForInstanceFailsWhenTheDatabaseCannotBeReached(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := probe.NewSQLRepository(db)
		owner, _ := twoProbes(t, repo)
		quietBefore := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
		for _, id := range []string{"a", "b"} {
			require.NoError(t, repo.UpsertInstance(context.Background(), owner.ID, &probe.Instance{ID: id, LastSeenAt: quietBefore.Add(-time.Hour)}))
		}
		gone, cancel := context.WithCancel(context.Background())
		cancel()

		err := repo.MakeRoomForInstance(gone, owner.ID, "c", 2, quietBefore)

		assert.ErrorIs(t, err, context.Canceled)
		held, err := repo.FindInstances(context.Background(), owner.ID)
		require.NoError(t, err)
		assert.Len(t, held, 2, "nothing was deleted")
	})
}
