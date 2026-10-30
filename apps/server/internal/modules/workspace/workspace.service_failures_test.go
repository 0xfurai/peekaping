package workspace_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/installsettings"
	"peekaping/internal/limits"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

var errBroken = errors.New("the database is broken")

// brokenMembers is a member repository whose reads fail, or answer as the test says, where it is told to
type brokenMembers struct {
	workspace.MemberRepository
	userEmail       error
	findMember      error
	emailVerified   error
	lockVerifiedErr error
	// lockVerified is what LockEmailVerified answers when it does not fail; nil leaves it to the database
	lockVerified *bool
}

func (m brokenMembers) UserEmail(ctx context.Context, userID string) (string, error) {
	if m.userEmail != nil {
		return "", m.userEmail
	}
	return m.MemberRepository.UserEmail(ctx, userID)
}

func (m brokenMembers) FindMember(ctx context.Context, id workspace.ID, userID string) (*workspace.Member, error) {
	if m.findMember != nil {
		return nil, m.findMember
	}
	return m.MemberRepository.FindMember(ctx, id, userID)
}

func (m brokenMembers) EmailVerified(ctx context.Context, userID string) (bool, error) {
	if m.emailVerified != nil {
		return false, m.emailVerified
	}
	return m.MemberRepository.EmailVerified(ctx, userID)
}

func (m brokenMembers) LockEmailVerified(ctx context.Context, userID string) (bool, error) {
	if m.lockVerifiedErr != nil {
		return false, m.lockVerifiedErr
	}
	if m.lockVerified != nil {
		return *m.lockVerified, nil
	}
	return m.MemberRepository.LockEmailVerified(ctx, userID)
}

// brokenWorkspaces is a workspace repository whose writes fail where it is told to
type brokenWorkspaces struct {
	workspace.Repository
	create          error
	createSuspended error
	addMember       error
	lift            error
	liftForOwner    error
}

func (w brokenWorkspaces) Create(ctx context.Context, name string) (*workspace.Workspace, error) {
	if w.create != nil {
		return nil, w.create
	}
	return w.Repository.Create(ctx, name)
}

func (w brokenWorkspaces) CreateSuspended(ctx context.Context, name, reason string) (*workspace.Workspace, error) {
	if w.createSuspended != nil {
		return nil, w.createSuspended
	}
	return w.Repository.CreateSuspended(ctx, name, reason)
}

func (w brokenWorkspaces) AddMember(ctx context.Context, id workspace.ID, userID string, role workspace.Role) error {
	if w.addMember != nil {
		return w.addMember
	}
	return w.Repository.AddMember(ctx, id, userID, role)
}

func (w brokenWorkspaces) LiftUnverifiedOwnedBy(ctx context.Context, userID string) ([]workspace.ID, error) {
	if w.lift != nil {
		return nil, w.lift
	}
	return w.Repository.LiftUnverifiedOwnedBy(ctx, userID)
}

func (w brokenWorkspaces) LiftUnverifiedWithVerifiedOwner(ctx context.Context, id workspace.ID) ([]workspace.ID, error) {
	if w.liftForOwner != nil {
		return nil, w.liftForOwner
	}
	return w.Repository.LiftUnverifiedWithVerifiedOwner(ctx, id)
}

// brokenInvites is an invite repository that fails to mark an invite accepted, or says it was marked
// by another accept already
type brokenInvites struct {
	workspace.InviteRepository
	markAccepted error
	alreadyUsed  bool
}

func (i brokenInvites) MarkAccepted(ctx context.Context, inviteID, userID string, at time.Time) (bool, error) {
	if i.markAccepted != nil {
		return false, i.markAccepted
	}
	if i.alreadyUsed {
		return false, nil
	}
	return i.InviteRepository.MarkAccepted(ctx, inviteID, userID, at)
}

// brokenLimits is a limits source that can't be read
type brokenLimits struct{ limits.Source }

func (brokenLimits) For(context.Context, workspace.ID) (limits.Limits, limits.Origin, error) {
	return limits.Limits{}, "", errBroken
}

func (brokenLimits) Snapshot(context.Context) (limits.Snapshot, error) {
	return limits.Snapshot{}, errBroken
}

func newServiceOver(db *bun.DB, repo workspace.Repository, members workspace.MemberRepository, invites workspace.InviteRepository, signup string, opts ...workspace.ServiceOption) workspace.Service {
	return workspace.NewService(repo, members, invites, dbtx.NewSQLTransactor(db), eventstest.NewBus(),
		installsettings.Static(&config.Config{ClientURL: "http://localhost:3000", SignupMode: signup}), nil, &wakeRecorder{}, zap.NewNop().Sugar(), opts...)
}

// isMember is whether the user is a member of the workspace
func isMember(t *testing.T, repo workspace.Repository, id workspace.ID, userID string) bool {
	t.Helper()
	memberships, err := repo.ListMemberships(context.Background(), userID)
	require.NoError(t, err)
	for _, m := range memberships {
		if m.Workspace.ID == id {
			return true
		}
	}
	return false
}

// inviteTo makes an invite of bob@example.com to the workspace, and returns its token
func inviteTo(t *testing.T, db *bun.DB, repo workspace.Repository, role workspace.Role) (workspace.ID, string, string) {
	t.Helper()
	ctx := context.Background()
	bob := insertUser(t, db, "bob@example.com")
	acme, err := repo.Create(ctx, "Acme")
	require.NoError(t, err)
	service := newServiceOver(db, repo, workspace.NewSQLMemberRepository(db), workspace.NewSQLInviteRepository(db), "")
	created, err := service.CreateInvite(workspace.WithContext(ctx, acme.ID, workspace.RoleOwner), "", "bob@example.com", role)
	require.NoError(t, err)
	return acme.ID, created.Token, bob
}

func TestService_AcceptInvite_AFailureAtAnyStepRefusesTheAcceptAndLeavesTheInviteForAnother(t *testing.T) {
	steps := map[string]func(db *bun.DB, repo workspace.Repository) workspace.Service{
		"the user's email can't be read": func(db *bun.DB, repo workspace.Repository) workspace.Service {
			return newServiceOver(db, repo, brokenMembers{MemberRepository: workspace.NewSQLMemberRepository(db), userEmail: errBroken}, workspace.NewSQLInviteRepository(db), "")
		},
		"the user's place can't be read": func(db *bun.DB, repo workspace.Repository) workspace.Service {
			return newServiceOver(db, repo, brokenMembers{MemberRepository: workspace.NewSQLMemberRepository(db), findMember: errBroken}, workspace.NewSQLInviteRepository(db), "")
		},
		"the workspace's limits can't be read": func(db *bun.DB, repo workspace.Repository) workspace.Service {
			return newServiceOver(db, repo, workspace.NewSQLMemberRepository(db), workspace.NewSQLInviteRepository(db), "", workspace.WithLimits(brokenLimits{}))
		},
		"the member can't be added": func(db *bun.DB, repo workspace.Repository) workspace.Service {
			return newServiceOver(db, brokenWorkspaces{Repository: repo, addMember: errBroken}, workspace.NewSQLMemberRepository(db), workspace.NewSQLInviteRepository(db), "")
		},
		"the invite can't be marked accepted": func(db *bun.DB, repo workspace.Repository) workspace.Service {
			return newServiceOver(db, repo, workspace.NewSQLMemberRepository(db), brokenInvites{InviteRepository: workspace.NewSQLInviteRepository(db), markAccepted: errBroken}, "")
		},
	}
	for name, build := range steps {
		t.Run(name, func(t *testing.T) {
			forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
				id, token, bob := inviteTo(t, db, repo, workspace.RoleMember)

				joined, err := build(db, repo).AcceptInvite(context.Background(), token, bob)

				assert.ErrorIs(t, err, errBroken)
				assert.Nil(t, joined)
				assert.False(t, isMember(t, repo, id, bob), "nothing is left of the accept")
				preview, err := newServiceOver(db, repo, workspace.NewSQLMemberRepository(db), workspace.NewSQLInviteRepository(db), "").PreviewInvite(context.Background(), token)
				require.NoError(t, err)
				assert.Equal(t, workspace.InvitePending, preview.Status, "the invite can still be used")
			})
		})
	}
}

// Of two accepts that race for one invite, the one that finds it used rolls back, member and all
func TestService_AcceptInvite_AnInviteUsedMeanwhileRollsTheMembershipBack(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		id, token, bob := inviteTo(t, db, repo, workspace.RoleMember)
		service := newServiceOver(db, repo, workspace.NewSQLMemberRepository(db), brokenInvites{InviteRepository: workspace.NewSQLInviteRepository(db), alreadyUsed: true}, "")

		joined, err := service.AcceptInvite(context.Background(), token, bob)

		assert.ErrorIs(t, err, workspace.ErrInviteUsed)
		assert.ErrorIs(t, err, workspace.ErrInviteUnavailable)
		assert.Nil(t, joined)
		assert.False(t, isMember(t, repo, id, bob))
	})
}

func TestService_AcceptInvite_AnOwnerInviteThatCannotLiftItsSuspensionFailsTheAccept(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		id, token, bob := inviteTo(t, db, repo, workspace.RoleOwner)
		service := newServiceOver(db, brokenWorkspaces{Repository: repo, liftForOwner: errBroken}, workspace.NewSQLMemberRepository(db), workspace.NewSQLInviteRepository(db), "")

		joined, err := service.AcceptInvite(context.Background(), token, bob)

		assert.ErrorIs(t, err, errBroken)
		assert.Nil(t, joined)
		assert.False(t, isMember(t, repo, id, bob), "the owner is not made")
	})
}

func TestService_AcceptInvite_AMemberInviteNeverTriesToLiftASuspension(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		id, token, bob := inviteTo(t, db, repo, workspace.RoleAdmin)
		service := newServiceOver(db, brokenWorkspaces{Repository: repo, liftForOwner: errBroken}, workspace.NewSQLMemberRepository(db), workspace.NewSQLInviteRepository(db), "")

		joined, err := service.AcceptInvite(context.Background(), token, bob)

		require.NoError(t, err)
		assert.Equal(t, id, joined.Workspace.ID)
		assert.Equal(t, "Acme", joined.Workspace.Name)
		assert.Equal(t, workspace.RoleAdmin, joined.Role)
		assert.WithinDuration(t, time.Now(), joined.JoinedAt, time.Minute)
		assert.True(t, isMember(t, repo, id, bob))
	})
}

func TestService_CreateWorkspace_AFailureAtAnyStepMakesNoWorkspace(t *testing.T) {
	type build func(db *bun.DB, repo workspace.Repository) workspace.Service
	steps := map[string]build{
		"the install's limits can't be read": func(db *bun.DB, repo workspace.Repository) workspace.Service {
			return newServiceOver(db, repo, workspace.NewSQLMemberRepository(db), workspace.NewSQLInviteRepository(db), "", workspace.WithLimits(brokenLimits{}))
		},
		"the workspace can't be made": func(db *bun.DB, repo workspace.Repository) workspace.Service {
			return newServiceOver(db, brokenWorkspaces{Repository: repo, create: errBroken}, workspace.NewSQLMemberRepository(db), workspace.NewSQLInviteRepository(db), "")
		},
		"the owner can't be added": func(db *bun.DB, repo workspace.Repository) workspace.Service {
			return newServiceOver(db, brokenWorkspaces{Repository: repo, addMember: errBroken}, workspace.NewSQLMemberRepository(db), workspace.NewSQLInviteRepository(db), "")
		},
		"the email of the user of an open install can't be read": func(db *bun.DB, repo workspace.Repository) workspace.Service {
			return newServiceOver(db, repo, brokenMembers{MemberRepository: workspace.NewSQLMemberRepository(db), emailVerified: errBroken}, workspace.NewSQLInviteRepository(db), config.SignupModeOpen)
		},
		"the suspended workspace of an open install can't be made": func(db *bun.DB, repo workspace.Repository) workspace.Service {
			return newServiceOver(db, brokenWorkspaces{Repository: repo, createSuspended: errBroken}, workspace.NewSQLMemberRepository(db), workspace.NewSQLInviteRepository(db), config.SignupModeOpen)
		},
		"the owner of a suspended workspace can't be added": func(db *bun.DB, repo workspace.Repository) workspace.Service {
			return newServiceOver(db, brokenWorkspaces{Repository: repo, addMember: errBroken}, workspace.NewSQLMemberRepository(db), workspace.NewSQLInviteRepository(db), config.SignupModeOpen)
		},
		"the second reading of the email can't be done": func(db *bun.DB, repo workspace.Repository) workspace.Service {
			return newServiceOver(db, repo, brokenMembers{MemberRepository: workspace.NewSQLMemberRepository(db), lockVerifiedErr: errBroken}, workspace.NewSQLInviteRepository(db), config.SignupModeOpen)
		},
		"a suspension that can't be lifted after the email is found verified": func(db *bun.DB, repo workspace.Repository) workspace.Service {
			verified := true
			return newServiceOver(db, brokenWorkspaces{Repository: repo, lift: errBroken}, brokenMembers{MemberRepository: workspace.NewSQLMemberRepository(db), lockVerified: &verified}, workspace.NewSQLInviteRepository(db), config.SignupModeOpen)
		},
	}
	for name, build := range steps {
		t.Run(name, func(t *testing.T) {
			forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
				user := insertUser(t, db, "maker@example.com")

				made, err := build(db, repo).CreateWorkspace(context.Background(), user, "Never")

				assert.ErrorIs(t, err, errBroken)
				assert.Nil(t, made)
				memberships, err := repo.ListMemberships(context.Background(), user)
				require.NoError(t, err)
				assert.Empty(t, memberships, "no workspace of the user is left")
				var count int
				require.NoError(t, db.NewSelect().Table("workspaces").ColumnExpr("COUNT(*)").Where("name = ?", "Never").Scan(context.Background(), &count))
				assert.Zero(t, count, "and none without an owner either")
			})
		})
	}
}

func TestService_CreateWorkspace_TheLimitsErrorSaysWhatItReads(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		user := insertUser(t, db, "maker@example.com")
		service := newServiceOver(db, repo, workspace.NewSQLMemberRepository(db), workspace.NewSQLInviteRepository(db), "", workspace.WithLimits(brokenLimits{}))

		_, err := service.CreateWorkspace(context.Background(), user, "Never")

		assert.EqualError(t, err, "read the install's limits: "+errBroken.Error())
	})
}

// A user whose email is verified when the second reading is done gets a workspace that runs, and the
// suspension the first reading made is lifted, in the database too
func TestService_CreateWorkspace_AnEmailVerifiedByTheSecondReadingLeavesTheWorkspaceRunning(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		user := insertUser(t, db, "maker@example.com")
		verified := true
		service := newServiceOver(db, repo, brokenMembers{MemberRepository: workspace.NewSQLMemberRepository(db), lockVerified: &verified}, workspace.NewSQLInviteRepository(db), config.SignupModeOpen)

		made, err := service.CreateWorkspace(ctx, user, "  Racing  ")

		require.NoError(t, err)
		assert.Equal(t, "Racing", made.Workspace.Name, "the name is trimmed")
		assert.Equal(t, workspace.RoleOwner, made.Role)
		assert.Nil(t, made.Workspace.SuspendedAt)
		assert.Empty(t, made.Workspace.SuspendedReason)
		found, err := repo.FindByID(ctx, made.Workspace.ID)
		require.NoError(t, err)
		assert.False(t, found.Suspended())
	})
}

// And one whose email is still not verified keeps the suspension the open install gives
func TestService_CreateWorkspace_AnEmailStillUnverifiedAtTheSecondReadingKeepsTheSuspension(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		user := insertUser(t, db, "maker@example.com")
		service := newServiceOver(db, repo, workspace.NewSQLMemberRepository(db), workspace.NewSQLInviteRepository(db), config.SignupModeOpen)

		made, err := service.CreateWorkspace(ctx, user, "Waiting")

		require.NoError(t, err)
		require.NotNil(t, made.Workspace.SuspendedAt)
		assert.Equal(t, workspace.SuspendedUnverified, made.Workspace.SuspendedReason)
		assert.Equal(t, made.Workspace.CreatedAt, made.JoinedAt)
	})
}

// A user whose email is verified at the first reading gets a workspace that is never suspended, and
// an install that is not open never reads the email
func TestService_CreateWorkspace_ReadsTheEmailOnlyInAnOpenInstall(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		user := insertUser(t, db, "maker@example.com")
		closed := newServiceOver(db, repo, brokenMembers{MemberRepository: workspace.NewSQLMemberRepository(db), emailVerified: errBroken, lockVerifiedErr: errBroken}, workspace.NewSQLInviteRepository(db), "")

		made, err := closed.CreateWorkspace(ctx, user, "Plain")

		require.NoError(t, err)
		assert.Nil(t, made.Workspace.SuspendedAt)

		_, err = db.ExecContext(ctx, "UPDATE users SET email_verified_at = ? WHERE id = ?", time.Now().UTC(), user)
		require.NoError(t, err)
		open := newServiceOver(db, repo, brokenMembers{MemberRepository: workspace.NewSQLMemberRepository(db), lockVerifiedErr: errBroken}, workspace.NewSQLInviteRepository(db), config.SignupModeOpen)

		made, err = open.CreateWorkspace(ctx, user, "Verified")

		require.NoError(t, err, "a verified user is not read a second time")
		assert.Nil(t, made.Workspace.SuspendedAt)
	})
}
