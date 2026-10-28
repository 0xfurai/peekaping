package monitor

import (
	"context"
	"testing"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func monitorWithToken(name, token string) *shared.Monitor {
	monitor := createTestMonitor(name, true, shared.MonitorStatusPending)
	monitor.PushToken = token
	return monitor
}

// storedToken is the push_token column of a monitor, nil for NULL
func storedToken(t *testing.T, db *bun.DB, id string) *string {
	t.Helper()
	var token *string
	require.NoError(t, db.NewSelect().Table("monitors").Column("push_token").Where("id = ?", id).Scan(context.Background(), &token))
	return token
}

func TestSQLRepository_MonitorsWithoutAPushTokenStoreNullAndReadBackEmpty(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()

		first, err := repo.Create(ctx, monitorWithToken("First", ""))
		require.NoError(t, err)
		second, err := repo.Create(ctx, monitorWithToken("Second", ""))
		require.NoError(t, err, "any number of monitors have no token")

		for _, m := range []*Model{first, second} {
			assert.Nil(t, storedToken(t, db, m.ID))
			found, err := repo.FindByID(ctx, m.ID)
			require.NoError(t, err)
			assert.Empty(t, found.PushToken)
		}

		// An update that takes the token away stores NULL as well, in both kinds of update
		withToken, err := repo.Create(ctx, monitorWithToken("With", "a-token"))
		require.NoError(t, err)
		require.NotNil(t, storedToken(t, db, withToken.ID))
		cleared := *withToken
		cleared.PushToken = ""
		require.NoError(t, repo.UpdateFull(ctx, withToken.ID, &cleared))
		assert.Nil(t, storedToken(t, db, withToken.ID))

		again := "again-token"
		require.NoError(t, repo.UpdatePartial(ctx, withToken.ID, &UpdateModel{PushToken: &again}))
		assert.Equal(t, &again, storedToken(t, db, withToken.ID))
		empty := ""
		require.NoError(t, repo.UpdatePartial(ctx, withToken.ID, &UpdateModel{PushToken: &empty}))
		assert.Nil(t, storedToken(t, db, withToken.ID))
	})
}

func TestSQLRepository_APushTokenAnotherMonitorHoldsIsRefused(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		held, err := repo.Create(ctx, monitorWithToken("Holder", "held-token"))
		require.NoError(t, err)
		other, err := repo.Create(ctx, monitorWithToken("Other", "other-token"))
		require.NoError(t, err)
		var taken *PushTokenAlreadyUsedError

		_, err = repo.Create(ctx, monitorWithToken("Newcomer", "held-token"))
		assert.ErrorAs(t, err, &taken, "on create")

		clash := *other
		clash.PushToken = "held-token"
		assert.ErrorAs(t, repo.UpdateFull(ctx, other.ID, &clash), &taken, "on a full update")

		token := "held-token"
		assert.ErrorAs(t, repo.UpdatePartial(ctx, other.ID, &UpdateModel{PushToken: &token}), &taken, "on a partial update")

		// A monitor may keep its own token
		own := *held
		own.Name = "Holder, renamed"
		assert.NoError(t, repo.UpdateFull(ctx, held.ID, &own))
		assert.Equal(t, "other-token", mustFind(t, repo, other.ID).PushToken)
	})
}

func mustFind(t *testing.T, repo MonitorRepository, id string) *Model {
	t.Helper()
	found, err := repo.FindByID(context.Background(), id)
	require.NoError(t, err)
	return found
}
