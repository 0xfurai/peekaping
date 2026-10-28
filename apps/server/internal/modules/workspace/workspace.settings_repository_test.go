package workspace_test

import (
	"context"
	"testing"

	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestWorkspaceSettings_EachWorkspaceReadsAndWritesItsOwn(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		first, err := repo.Create(ctx, "First")
		require.NoError(t, err)
		second, err := repo.Create(ctx, "Second")
		require.NoError(t, err)
		settings := setting.NewSQLWorkspaceRepository(db)

		got, err := settings.GetByKey(ctx, first.ID, setting.DataRetentionPeriodKey)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "180", got.Value, "a new workspace starts with the default")

		set, err := settings.SetByKey(ctx, first.ID, setting.DataRetentionPeriodKey, &setting.Model{Value: "1", Type: "int"})
		require.NoError(t, err)
		assert.Equal(t, setting.Model{Key: setting.DataRetentionPeriodKey, Value: "1", Type: "int", CreatedAt: set.CreatedAt, UpdatedAt: set.UpdatedAt}, *set)
		_, err = settings.SetByKey(ctx, first.ID, setting.DataRetentionPeriodKey, &setting.Model{Value: "2", Type: "int"})
		require.NoError(t, err)

		mine, err := settings.GetByKey(ctx, first.ID, setting.DataRetentionPeriodKey)
		require.NoError(t, err)
		assert.Equal(t, "2", mine.Value, "a second write replaces the first")
		theirs, err := settings.GetByKey(ctx, second.ID, setting.DataRetentionPeriodKey)
		require.NoError(t, err)
		assert.Equal(t, "180", theirs.Value, "and the other workspace keeps its own")

		require.NoError(t, settings.DeleteByKey(ctx, first.ID, setting.DataRetentionPeriodKey))
		gone, err := settings.GetByKey(ctx, first.ID, setting.DataRetentionPeriodKey)
		require.NoError(t, err)
		assert.Nil(t, gone)
		kept, err := settings.GetByKey(ctx, second.ID, setting.DataRetentionPeriodKey)
		require.NoError(t, err)
		assert.NotNil(t, kept, "a delete reaches one workspace")

		_, err = settings.SetByKey(ctx, first.ID, setting.DataRetentionPeriodKey, &setting.Model{Value: "7", Type: "int"})
		require.NoError(t, err)
		again, err := settings.GetByKey(ctx, first.ID, setting.DataRetentionPeriodKey)
		require.NoError(t, err)
		assert.Equal(t, "7", again.Value, "and a key that was deleted can be set again")
	})
}
