package workspace_test

import (
	"context"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// blindMembers is a member repository whose FindMember sees nobody: it is what an accept finds
// when the user becomes a member after it looked and before it adds them.
type blindMembers struct{ workspace.MemberRepository }

func (blindMembers) FindMember(context.Context, workspace.ID, string) (*workspace.Member, error) {
	return nil, nil
}

// Two accepts of one invite by one user at once: both look and find no membership, one adds it
// and the other meets the unique key. That one is told what a later accept is told, 409
// ErrAlreadyMember, and not an unexpected failure.
func TestService_AcceptInvite_AMembershipAddedMeanwhileIsAlreadyAMemberAndNotAnUnexpectedFailure(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		bob := insertUser(t, db, "bob@example.com")
		acme, err := repo.Create(ctx, "Acme")
		require.NoError(t, err)
		service := workspace.NewService(repo, blindMembers{workspace.NewSQLMemberRepository(db)}, workspace.NewSQLInviteRepository(db),
			dbtx.NewSQLTransactor(db), eventstest.NewBus(), &config.Config{ClientURL: "http://localhost:3000"}, zap.NewNop().Sugar())
		created, err := service.CreateInvite(workspace.WithContext(ctx, acme.ID, workspace.RoleOwner), "", "bob@example.com", workspace.RoleMember)
		require.NoError(t, err)
		require.NoError(t, repo.AddMember(ctx, acme.ID, bob, workspace.RoleAdmin), "the other accept won")

		joined, err := service.AcceptInvite(ctx, created.Token, bob)

		assert.ErrorIs(t, err, workspace.ErrAlreadyMember)
		assert.Nil(t, joined)
		preview, err := service.PreviewInvite(ctx, created.Token)
		require.NoError(t, err)
		assert.Equal(t, workspace.InvitePending, preview.Status, "and the invite is not spent")
	})
}
