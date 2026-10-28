package workspace_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"

	"peekaping/cmd/bun/migrations"
)

// forEachBackend runs fn on a repository over a migrated SQLite and PostgreSQL database.
func forEachBackend(t *testing.T, fn func(t *testing.T, db *bun.DB, repo workspace.Repository)) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(context.Background()))
			_, err := migrator.Migrate(context.Background())
			require.NoError(t, err)
			fn(t, db, workspace.NewSQLRepository(db))
		})
	}
}

func TestRepository_CreateAndAddMember(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		user := insertUser(t, db, "owner@example.com")

		created, err := repo.Create(ctx, "Acme")
		require.NoError(t, err)
		assert.NotEmpty(t, created.ID)
		assert.Equal(t, "Acme", created.Name)
		require.NoError(t, repo.AddMember(ctx, created.ID, user, workspace.RoleOwner))

		memberships, err := repo.ListMemberships(ctx, user)
		require.NoError(t, err)
		require.Len(t, memberships, 1)
		assert.Equal(t, created.ID, memberships[0].Workspace.ID)
		assert.Equal(t, "Acme", memberships[0].Workspace.Name)
		assert.WithinDuration(t, time.Now(), memberships[0].Workspace.CreatedAt, time.Minute)
		assert.Equal(t, workspace.RoleOwner, memberships[0].Role)
		assert.WithinDuration(t, time.Now(), memberships[0].JoinedAt, time.Minute)

		assert.Error(t, repo.AddMember(ctx, created.ID, user, workspace.RoleMember), "a user is a member of a workspace once")
	})
}

func TestRepository_ListMemberships_ListsTheFirstJoinedWorkspaceFirst(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		user, other := insertUser(t, db, "user@example.com"), insertUser(t, db, "other@example.com")
		// The workspace created last is the one the user joined first
		late, err := repo.Create(ctx, "Late")
		require.NoError(t, err)
		early, err := repo.Create(ctx, "Early")
		require.NoError(t, err)
		unrelated, err := repo.Create(ctx, "Unrelated")
		require.NoError(t, err)
		require.NoError(t, repo.AddMember(ctx, early.ID, user, workspace.RoleAdmin))
		time.Sleep(5 * time.Millisecond)
		require.NoError(t, repo.AddMember(ctx, late.ID, user, workspace.RoleMember))
		require.NoError(t, repo.AddMember(ctx, unrelated.ID, other, workspace.RoleOwner))

		memberships, err := repo.ListMemberships(ctx, user)

		require.NoError(t, err)
		require.Len(t, memberships, 2)
		assert.Equal(t, early.ID, memberships[0].Workspace.ID, "the default workspace")
		assert.Equal(t, workspace.RoleAdmin, memberships[0].Role)
		assert.Equal(t, late.ID, memberships[1].Workspace.ID)
		assert.Equal(t, workspace.RoleMember, memberships[1].Role, "a role for each workspace")

		none, err := repo.ListMemberships(ctx, insertUser(t, db, "alone@example.com"))
		require.NoError(t, err)
		assert.Empty(t, none)
	})
}

func TestRepository_FindOldest(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		none, err := repo.FindOldest(ctx)
		require.NoError(t, err)
		assert.Nil(t, none, "an install with no workspace")

		first, err := repo.Create(ctx, "First")
		require.NoError(t, err)
		time.Sleep(5 * time.Millisecond)
		_, err = repo.Create(ctx, "Second")
		require.NoError(t, err)

		oldest, err := repo.FindOldest(ctx)
		require.NoError(t, err)
		require.NotNil(t, oldest)
		assert.Equal(t, first.ID, oldest.ID)
		assert.Equal(t, "First", oldest.Name)
	})
}

func TestRepository_JoinsTheTransactionInTheContext(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		user := insertUser(t, db, "user@example.com")
		boom := errors.New("boom")

		err := dbtx.NewSQLTransactor(db).WithTx(ctx, func(ctx context.Context) error {
			created, err := repo.Create(ctx, "Rolled back")
			require.NoError(t, err)
			require.NoError(t, repo.AddMember(ctx, created.ID, user, workspace.RoleOwner))
			return boom
		})

		require.ErrorIs(t, err, boom)
		assert.Empty(t, workspaces(t, db))
		assert.Empty(t, members(t, db))
	})
}
