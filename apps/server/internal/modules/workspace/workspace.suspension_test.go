package workspace_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/installsettings"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"go.uber.org/zap"
)

// PP-155: a workspace is suspended with a reason, and a verified email lifts the suspensions that
// the reason 'unverified' made, for the workspaces its user owns.

func TestRepository_ANewWorkspaceRuns(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		created, err := repo.Create(context.Background(), "Runs")
		require.NoError(t, err)

		found, err := repo.FindByID(context.Background(), created.ID)
		require.NoError(t, err)
		assert.False(t, found.Suspended())
		assert.Nil(t, found.SuspendedAt)
		assert.Empty(t, found.SuspendedReason)
	})
}

func TestRepository_CreateSuspendedStoresWhenAndWhy(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		user := insertUser(t, db, "owner@example.com")

		created, err := repo.CreateSuspended(ctx, "Waiting", workspace.SuspendedUnverified)
		require.NoError(t, err)
		require.NoError(t, repo.AddMember(ctx, created.ID, user, workspace.RoleOwner))

		assert.True(t, created.Suspended())
		found, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		require.True(t, found.Suspended())
		assert.WithinDuration(t, time.Now(), *found.SuspendedAt, time.Minute)
		assert.Equal(t, workspace.SuspendedUnverified, found.SuspendedReason)
		memberships, err := repo.ListMemberships(ctx, user)
		require.NoError(t, err)
		require.Len(t, memberships, 1)
		assert.True(t, memberships[0].Workspace.Suspended(), "the user's list says it too")
		assert.Equal(t, workspace.SuspendedUnverified, memberships[0].Workspace.SuspendedReason)
	})
}

func TestRepository_LiftUnverifiedOwnedBy(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		user, other := insertUser(t, db, "user@example.com"), insertUser(t, db, "other@example.com")
		owned, err := repo.CreateSuspended(ctx, "Owned", workspace.SuspendedUnverified)
		require.NoError(t, err)
		onlyAdmin, err := repo.CreateSuspended(ctx, "Admin of", workspace.SuspendedUnverified)
		require.NoError(t, err)
		othersOwn, err := repo.CreateSuspended(ctx, "Others", workspace.SuspendedUnverified)
		require.NoError(t, err)
		heldByAdmin, err := repo.CreateSuspended(ctx, "Held", workspace.SuspendedUnverified)
		require.NoError(t, err)
		running, err := repo.Create(ctx, "Running")
		require.NoError(t, err)
		require.NoError(t, repo.AddMember(ctx, owned.ID, user, workspace.RoleOwner))
		require.NoError(t, repo.AddMember(ctx, onlyAdmin.ID, user, workspace.RoleAdmin))
		require.NoError(t, repo.AddMember(ctx, othersOwn.ID, other, workspace.RoleOwner))
		require.NoError(t, repo.AddMember(ctx, heldByAdmin.ID, user, workspace.RoleOwner))
		require.NoError(t, repo.AddMember(ctx, running.ID, user, workspace.RoleOwner))
		insertMonitor := func(workspaceID workspace.ID) string {
			id := uuid.NewString()
			_, err := db.ExecContext(ctx, `INSERT INTO monitors (id, workspace_id, type, name, interval, timeout, max_retries, retry_interval, resend_interval, updated_at) VALUES (?, ?, 'http', 'Monitor', 60, 10, 1, 10, 0, '2020-01-01 00:00:00')`, id, string(workspaceID))
			require.NoError(t, err)
			return id
		}
		inLifted, inHeld := insertMonitor(owned.ID), insertMonitor(othersOwn.ID)
		// A suspension for another reason (PP-372's) is not an unverified one: verifying does not lift it
		_, err = db.ExecContext(ctx, "UPDATE workspaces SET suspended_reason = 'admin' WHERE id = ?", string(heldByAdmin.ID))
		require.NoError(t, err)

		lifted, err := repo.LiftUnverifiedOwnedBy(ctx, user)

		require.NoError(t, err)
		assert.Equal(t, []workspace.ID{owned.ID}, lifted)
		suspended := func(id workspace.ID) bool {
			found, err := repo.FindByID(ctx, id)
			require.NoError(t, err)
			return found.Suspended()
		}
		assert.False(t, suspended(owned.ID))
		assert.True(t, suspended(onlyAdmin.ID), "a workspace the user does not own stays suspended")
		assert.True(t, suspended(othersOwn.ID), "so does another user's")
		assert.True(t, suspended(heldByAdmin.ID), "so does one suspended for another reason")

		updatedAt := func(id string) time.Time {
			var at time.Time
			require.NoError(t, db.NewSelect().Table("monitors").Column("updated_at").Where("id = ?", id).Scan(ctx, &at))
			return at
		}
		assert.WithinDuration(t, time.Now(), updatedAt(inLifted), time.Minute, "a monitor that starts to run is not stale for the days it was held back")
		assert.Equal(t, 2020, updatedAt(inHeld).Year(), "a monitor of a workspace that stays suspended is untouched")

		again, err := repo.LiftUnverifiedOwnedBy(ctx, user)
		require.NoError(t, err)
		assert.Empty(t, again, "nothing is left to lift")
	})
}

// wakeRecorder is the MonitorWaker that remembers the workspaces it was asked to wake
type wakeRecorder struct{ woken []workspace.ID }

func (w *wakeRecorder) PublishActiveUpdated(_ context.Context, id workspace.ID) error {
	w.woken = append(w.woken, id)
	return nil
}

// suspendedWorkspace is a workspace suspended as unverified, with an unverified owner and an
// admin (verified or not); it returns the ids of both
func suspendedWorkspace(t *testing.T, db *bun.DB, repo workspace.Repository, adminVerified bool) (workspace.ID, string, string) {
	t.Helper()
	ctx := context.Background()
	owner, admin := insertUser(t, db, "owner@example.com"), insertUser(t, db, "admin@example.com")
	if adminVerified {
		_, err := db.ExecContext(ctx, "UPDATE users SET email_verified_at = ? WHERE id = ?", time.Now().UTC(), admin)
		require.NoError(t, err)
	}
	made, err := repo.CreateSuspended(ctx, "Waiting", workspace.SuspendedUnverified)
	require.NoError(t, err)
	require.NoError(t, repo.AddMember(ctx, made.ID, owner, workspace.RoleOwner))
	require.NoError(t, repo.AddMember(ctx, made.ID, admin, workspace.RoleAdmin))
	return made.ID, owner, admin
}

func newServiceWithWaker(db *bun.DB, repo workspace.Repository, waker workspace.MonitorWaker) workspace.Service {
	return workspace.NewService(repo, workspace.NewSQLMemberRepository(db), workspace.NewSQLInviteRepository(db),
		dbtx.NewSQLTransactor(db), eventstest.NewBus(), installsettings.Static(&config.Config{ClientURL: "http://localhost:3000"}), nil, waker, zap.NewNop().Sugar())
}

// A suspension ends when a verified user owns the workspace. Verifying lifts it for the owner who
// verifies; an unverified owner who makes a verified member an owner would leave it suspended for good.
func TestService_ChangeRole_MakingAVerifiedUserOwnerLiftsTheUnverifiedSuspension(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		id, _, admin := suspendedWorkspace(t, db, repo, true)
		_, err := db.ExecContext(ctx, `INSERT INTO monitors (id, workspace_id, type, name, interval, timeout, max_retries, retry_interval, resend_interval, updated_at) VALUES (?, ?, 'http', 'Monitor', 60, 10, 1, 10, 0, '2020-01-01 00:00:00')`, uuid.NewString(), string(id))
		require.NoError(t, err)
		waker := &wakeRecorder{}
		service := newServiceWithWaker(db, repo, waker)

		_, err = service.ChangeRole(workspace.WithContext(ctx, id, workspace.RoleOwner), admin, workspace.RoleOwner)

		require.NoError(t, err)
		found, err := repo.FindByID(ctx, id)
		require.NoError(t, err)
		assert.False(t, found.Suspended(), "the new owner is verified")
		assert.Equal(t, []workspace.ID{id}, waker.woken, "and its monitors are woken, as when an owner verifies")
		var at time.Time
		require.NoError(t, db.NewSelect().Table("monitors").Column("updated_at").Where("workspace_id = ?", string(id)).Scan(ctx, &at))
		assert.WithinDuration(t, time.Now(), at, time.Minute, "and do not count as stale for the days they were held back")
	})
}

func TestService_ChangeRole_MakingAnUnverifiedUserOwnerLeavesTheSuspension(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		id, _, admin := suspendedWorkspace(t, db, repo, false)
		waker := &wakeRecorder{}
		service := newServiceWithWaker(db, repo, waker)

		_, err := service.ChangeRole(workspace.WithContext(ctx, id, workspace.RoleOwner), admin, workspace.RoleOwner)

		require.NoError(t, err)
		found, err := repo.FindByID(ctx, id)
		require.NoError(t, err)
		assert.True(t, found.Suspended())
		assert.Empty(t, waker.woken)
	})
}

func TestService_ChangeRole_MakingAVerifiedUserAdminLeavesTheSuspension(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		id, _, admin := suspendedWorkspace(t, db, repo, true)
		service := newServiceWithWaker(db, repo, &wakeRecorder{})

		_, err := service.ChangeRole(workspace.WithContext(ctx, id, workspace.RoleOwner), admin, workspace.RoleMember)

		require.NoError(t, err)
		found, err := repo.FindByID(ctx, id)
		require.NoError(t, err)
		assert.True(t, found.Suspended(), "only an owner's verification counts")
	})
}

// An invite for the owner role is the other way a verified user becomes an owner
func TestService_AcceptInvite_AVerifiedUserJoiningAsOwnerLiftsTheUnverifiedSuspension(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		id, _, _ := suspendedWorkspace(t, db, repo, false)
		guest := insertUser(t, db, "guest@example.com")
		_, err := db.ExecContext(ctx, "UPDATE users SET email_verified_at = ? WHERE id = ?", time.Now().UTC(), guest)
		require.NoError(t, err)
		waker := &wakeRecorder{}
		service := newServiceWithWaker(db, repo, waker)
		created, err := service.CreateInvite(workspace.WithContext(ctx, id, workspace.RoleOwner), "", "guest@example.com", workspace.RoleOwner)
		require.NoError(t, err)

		_, err = service.AcceptInvite(ctx, created.Token, guest)

		require.NoError(t, err)
		found, err := repo.FindByID(ctx, id)
		require.NoError(t, err)
		assert.False(t, found.Suspended())
		assert.Equal(t, []workspace.ID{id}, waker.woken)
	})
}

// verifiesAfterTheFirstRead is a member repository whose first EmailVerified answers as the database
// does, and then lets the user verify their email in another transaction that commits before the
// caller goes on: the moment the creation of a workspace read "not verified" and the verification
// lifted nothing, since the workspace did not exist yet
type verifiesAfterTheFirstRead struct {
	workspace.MemberRepository
	verify func()
}

func (r *verifiesAfterTheFirstRead) EmailVerified(ctx context.Context, userID string) (bool, error) {
	verified, err := r.MemberRepository.EmailVerified(ctx, userID)
	if verify := r.verify; verify != nil {
		r.verify = nil
		verify()
	}
	return verified, err
}

func TestService_CreateWorkspace_AVerificationThatCommitsDuringTheCreationLeavesTheWorkspaceRunning(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		if db.Dialect().Name() != dialect.PG {
			// SQLite has one connection: a request's transaction holds it, so no other can commit between its steps
			t.Skip("the interleaving exists on PostgreSQL only")
		}
		ctx := context.Background()
		user := insertUser(t, db, "maker@example.com")
		members := &verifiesAfterTheFirstRead{MemberRepository: workspace.NewSQLMemberRepository(db)}
		members.verify = func() {
			// what verifying an email does: mark the address, then lift what the user owns
			_, err := db.ExecContext(ctx, "UPDATE users SET email_verified_at = ? WHERE id = ?", time.Now().UTC(), user)
			require.NoError(t, err)
			_, err = repo.LiftUnverifiedOwnedBy(ctx, user)
			require.NoError(t, err)
		}
		service := workspace.NewService(repo, members, workspace.NewSQLInviteRepository(db), dbtx.NewSQLTransactor(db), eventstest.NewBus(),
			installsettings.Static(&config.Config{ClientURL: "http://localhost:3000", SignupMode: config.SignupModeOpen}), nil, &wakeRecorder{}, zap.NewNop().Sugar())

		made, err := service.CreateWorkspace(ctx, user, "Racing")

		require.NoError(t, err)
		found, err := repo.FindByID(ctx, made.Workspace.ID)
		require.NoError(t, err)
		assert.False(t, found.Suspended(), "the owner is verified, and nothing is left to lift it")
		assert.Empty(t, found.SuspendedReason)
	})
}

func TestService_CreateWorkspace_AnUnverifiedUsersWorkspaceIsSuspendedInAnOpenInstall(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		user := insertUser(t, db, "maker@example.com")
		service := workspace.NewService(repo, workspace.NewSQLMemberRepository(db), workspace.NewSQLInviteRepository(db), dbtx.NewSQLTransactor(db), eventstest.NewBus(),
			installsettings.Static(&config.Config{ClientURL: "http://localhost:3000", SignupMode: config.SignupModeOpen}), nil, &wakeRecorder{}, zap.NewNop().Sugar())

		made, err := service.CreateWorkspace(ctx, user, "Waiting")

		require.NoError(t, err)
		found, err := repo.FindByID(ctx, made.Workspace.ID)
		require.NoError(t, err)
		assert.True(t, found.Suspended())
		assert.Equal(t, workspace.SuspendedUnverified, found.SuspendedReason)
	})
}

// PP-372: an instance admin's hold is its own, so a workspace can be under it and the verification
// one at once. The reason shown is the admin's first, then the verification's.
func TestWorkspace_ShownReasonIsTheAdminsBeforeTheVerificationsAndNoneWhenItRuns(t *testing.T) {
	now := time.Now()
	unverified := workspace.SuspendedUnverified
	cases := []struct {
		name      string
		ws        workspace.Workspace
		suspended bool
		reason    string
	}{
		{"runs", workspace.Workspace{}, false, ""},
		{"verification only", workspace.Workspace{SuspendedAt: &now, SuspendedReason: unverified}, true, workspace.SuspendedUnverified},
		{"admin only", workspace.Workspace{AdminSuspendedAt: &now}, true, workspace.SuspendedByAdmin},
		{"both", workspace.Workspace{SuspendedAt: &now, SuspendedReason: unverified, AdminSuspendedAt: &now}, true, workspace.SuspendedByAdmin},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.suspended, c.ws.Suspended())
			assert.Equal(t, c.reason, c.ws.ShownReason())
		})
	}
	assert.Equal(t, "admin", workspace.SuspendedByAdmin)
}

// lifting the verification hold leaves the admin's
func TestRepository_LiftUnverifiedLeavesTheAdminsHold(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		ctx := context.Background()
		user := insertUser(t, db, "owner@example.com")
		held, err := repo.CreateSuspended(ctx, "Held", workspace.SuspendedUnverified)
		require.NoError(t, err)
		require.NoError(t, repo.AddMember(ctx, held.ID, user, workspace.RoleOwner))
		_, err = db.Exec("UPDATE workspaces SET admin_suspended_at = ? WHERE id = ?", time.Now().UTC(), string(held.ID))
		require.NoError(t, err)

		lifted, err := repo.LiftUnverifiedOwnedBy(ctx, user)
		require.NoError(t, err)
		assert.Equal(t, []workspace.ID{held.ID}, lifted)

		found, err := repo.FindByID(ctx, held.ID)
		require.NoError(t, err)
		assert.Nil(t, found.SuspendedAt)
		assert.NotNil(t, found.AdminSuspendedAt, "the admin's hold stays")
		assert.True(t, found.Suspended())
		assert.Equal(t, workspace.SuspendedByAdmin, found.ShownReason())
	})
}

// verifiesDuringTheLift is a workspace repository whose first LiftUnverifiedWithVerifiedOwner
// answers as the database does, and then lets the user verify their email from another
// connection while the caller's transaction is still open: the moment a new owner's role was
// written but not committed, so the verification finds no workspace of theirs to lift. It waits
// for the verification for a moment only, since one that waits for the caller's lock goes on only
// once the caller commits.
type verifiesDuringTheLift struct {
	workspace.Repository
	verify   func() error
	verified chan error
}

func (r *verifiesDuringTheLift) LiftUnverifiedWithVerifiedOwner(ctx context.Context, id workspace.ID) ([]workspace.ID, error) {
	lifted, err := r.Repository.LiftUnverifiedWithVerifiedOwner(ctx, id)
	if verify := r.verify; verify != nil {
		r.verify = nil
		done := make(chan struct{})
		go func() {
			defer close(done)
			r.verified <- verify()
		}()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
		}
	}
	return lifted, err
}

// racingVerification is verifiesDuringTheLift for the user, with what verifying an email does: mark
// the address, then lift what the user owns
func racingVerification(db *bun.DB, repo workspace.Repository, user string) *verifiesDuringTheLift {
	return &verifiesDuringTheLift{
		Repository: repo,
		verified:   make(chan error, 1),
		verify: func() error {
			ctx := context.Background()
			if _, err := db.ExecContext(ctx, "UPDATE users SET email_verified_at = ? WHERE id = ?", time.Now().UTC(), user); err != nil {
				return err
			}
			_, err := repo.LiftUnverifiedOwnedBy(ctx, user)
			return err
		},
	}
}

// requireLiftedAfterTheVerification waits for the racing verification to end, and checks the
// workspace runs
func requireLiftedAfterTheVerification(t *testing.T, racing *verifiesDuringTheLift, id workspace.ID) {
	t.Helper()
	select {
	case err := <-racing.verified:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the verification never ended")
	}
	found, err := racing.Repository.FindByID(context.Background(), id)
	require.NoError(t, err)
	assert.False(t, found.Suspended(), "the new owner is verified, and nothing is left to lift it")
}

func TestService_ChangeRole_AVerificationThatCommitsDuringTheChangeLeavesTheWorkspaceRunning(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		if db.Dialect().Name() != dialect.PG {
			// SQLite has one connection: a request's transaction holds it, so no other can commit between its steps
			t.Skip("the interleaving exists on PostgreSQL only")
		}
		ctx := context.Background()
		id, _, admin := suspendedWorkspace(t, db, repo, false)
		racing := racingVerification(db, repo, admin)
		service := newServiceWithWaker(db, racing, &wakeRecorder{})

		_, err := service.ChangeRole(workspace.WithContext(ctx, id, workspace.RoleOwner), admin, workspace.RoleOwner)

		require.NoError(t, err)
		requireLiftedAfterTheVerification(t, racing, id)
	})
}

func TestService_AcceptInvite_AVerificationThatCommitsDuringTheAcceptLeavesTheWorkspaceRunning(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB, repo workspace.Repository) {
		if db.Dialect().Name() != dialect.PG {
			t.Skip("the interleaving exists on PostgreSQL only")
		}
		ctx := context.Background()
		id, _, _ := suspendedWorkspace(t, db, repo, false)
		guest := insertUser(t, db, "guest@example.com")
		racing := racingVerification(db, repo, guest)
		service := newServiceWithWaker(db, racing, &wakeRecorder{})
		created, err := service.CreateInvite(workspace.WithContext(ctx, id, workspace.RoleOwner), "", "guest@example.com", workspace.RoleOwner)
		require.NoError(t, err)

		_, err = service.AcceptInvite(ctx, created.Token, guest)

		require.NoError(t, err)
		requireLiftedAfterTheVerification(t, racing, id)
	})
}
