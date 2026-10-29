package apitest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
)

// PP-154: the members of a workspace, their roles, removing and leaving (docs/adr/0005 section 1).

type memberResponse struct {
	UserID   string `json:"userId"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	JoinedAt string `json:"joinedAt"`
}

const (
	membersPath = "/api/v1/workspace/members"
	leavePath   = "/api/v1/workspace/leave"
)

// team is the first workspace with its owner (the admin), a second owner, an admin and a member
type team struct {
	a                           *app
	id                          workspace.ID
	owner2, admin, member       credentials
	owner2ID, adminID, memberID string
}

func newTeam(t *testing.T, a *app) *team {
	t.Helper()
	tm := &team{a: a, id: a.firstWorkspace(t)}
	tm.owner2 = a.userIn(t, "owner2@example.com", tm.id, workspace.RoleOwner)
	tm.admin = a.userIn(t, "admin2@example.com", tm.id, workspace.RoleAdmin)
	tm.member = a.userIn(t, "member@example.com", tm.id, workspace.RoleMember)
	tm.owner2ID, tm.adminID, tm.memberID = a.userID(t, "owner2@example.com"), a.userID(t, "admin2@example.com"), a.userID(t, "member@example.com")
	return tm
}

func (tm *team) roleOf(t *testing.T, userID string) string {
	t.Helper()
	for _, m := range decode[[]memberResponse](t, tm.a.request(t, http.MethodGet, membersPath, nil, tm.a.asAdmin), http.StatusOK) {
		if m.UserID == userID {
			return m.Role
		}
	}
	return ""
}

func (tm *team) setRole(userID, role string, creds credentials, t *testing.T) *httpRecorder {
	t.Helper()
	return tm.a.request(t, http.MethodPatch, membersPath+"/"+userID, map[string]any{"role": role}, creds)
}

func TestMembers_EveryMemberSeesWhoIsInTheWorkspaceWithTheirRoles(t *testing.T) {
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)

		for name, creds := range map[string]credentials{"a member": tm.member, "an admin": tm.admin, "an owner": a.asAdmin, "an API key": a.withAPIKey} {
			t.Run(name, func(t *testing.T) {
				members := decode[[]memberResponse](t, a.request(t, http.MethodGet, membersPath, nil, creds), http.StatusOK)

				require.Len(t, members, 4)
				got := map[string]string{}
				for _, m := range members {
					got[m.Email] = m.Role
					assert.NotEmpty(t, m.UserID)
					assert.NotEmpty(t, m.JoinedAt)
				}
				assert.Equal(t, map[string]string{adminEmail: "owner", "owner2@example.com": "owner", "admin2@example.com": "admin", "member@example.com": "member"}, got)
				assert.Equal(t, adminEmail, members[0].Email, "the first to join comes first")
			})
		}
	})
}

func TestMembers_AnAdminChangesMembersAndAdminsAndNeverOwners(t *testing.T) {
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)

		// a member to admin, and back
		assert.Equal(t, "admin", decode[memberResponse](t, tm.setRole(tm.memberID, "admin", tm.admin, t), http.StatusOK).Role)
		assert.Equal(t, "admin", tm.roleOf(t, tm.memberID))
		assert.Equal(t, "member", decode[memberResponse](t, tm.setRole(tm.memberID, "member", tm.admin, t), http.StatusOK).Role)
		// another admin to member
		decode[memberResponse](t, tm.setRole(tm.adminID, "member", tm.admin, t), http.StatusOK)
		assert.Equal(t, "member", tm.roleOf(t, tm.adminID))
		decode[memberResponse](t, tm.setRole(tm.adminID, "admin", tm.a.asAdmin, t), http.StatusOK)

		// not an owner, not to owner
		assert.Equal(t, http.StatusForbidden, tm.setRole(tm.owner2ID, "member", tm.admin, t).Code, "an admin demotes no owner")
		assert.Equal(t, http.StatusForbidden, tm.setRole(tm.memberID, "owner", tm.admin, t).Code, "an admin makes no owner")
		assert.Equal(t, http.StatusForbidden, tm.setRole(tm.adminID, "owner", tm.admin, t).Code, "an admin does not make themselves an owner")
		assert.Equal(t, "owner", tm.roleOf(t, tm.owner2ID))
		assert.Equal(t, "member", tm.roleOf(t, tm.memberID))
	})
}

func TestMembers_AMemberChangesNobody(t *testing.T) {
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)

		assert.Equal(t, http.StatusForbidden, tm.setRole(tm.memberID, "admin", tm.member, t).Code, "not even their own role")
		assert.Equal(t, http.StatusForbidden, tm.setRole(tm.adminID, "member", tm.member, t).Code)
		assert.Equal(t, http.StatusForbidden, a.request(t, http.MethodDelete, membersPath+"/"+tm.adminID, nil, tm.member).Code)
		assert.Equal(t, "member", tm.roleOf(t, tm.memberID))
		assert.Equal(t, "admin", tm.roleOf(t, tm.adminID))
	})
}

func TestMembers_OnlyAnOwnerMakesDemotesOrRemovesOwners(t *testing.T) {
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)

		decode[memberResponse](t, tm.setRole(tm.memberID, "owner", a.asAdmin, t), http.StatusOK)
		assert.Equal(t, "owner", tm.roleOf(t, tm.memberID))
		decode[memberResponse](t, tm.setRole(tm.memberID, "admin", tm.owner2, t), http.StatusOK)
		assert.Equal(t, "admin", tm.roleOf(t, tm.memberID), "an owner demotes another owner")
		assert.Equal(t, http.StatusForbidden, a.request(t, http.MethodDelete, membersPath+"/"+tm.owner2ID, nil, tm.admin).Code, "an admin removes no owner")
		assert.Equal(t, http.StatusNoContent, a.request(t, http.MethodDelete, membersPath+"/"+tm.owner2ID, nil, a.asAdmin).Code, "an owner removes another owner")
		assert.Equal(t, "", tm.roleOf(t, tm.owner2ID))
	})
}

func TestMembers_TheLastOwnerCanNotLeaveBeRemovedOrBeDemoted(t *testing.T) {
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeamWithoutSecondOwner(t, a)
		require.Equal(t, 1, a.ownerCount(t, tm.id))
		adminID := a.userID(t, adminEmail)

		assert.Equal(t, http.StatusConflict, tm.setRole(adminID, "admin", a.asAdmin, t).Code, "demoted by themselves")
		assert.Equal(t, http.StatusConflict, a.request(t, http.MethodDelete, membersPath+"/"+adminID, nil, a.asAdmin).Code, "removed by themselves")
		assert.Equal(t, http.StatusConflict, a.request(t, http.MethodPost, leavePath, nil, a.asAdmin).Code, "leaving")
		assert.Equal(t, http.StatusForbidden, tm.setRole(adminID, "member", tm.admin, t).Code, "an admin can't touch an owner, the last included")
		assert.Equal(t, http.StatusForbidden, a.request(t, http.MethodDelete, membersPath+"/"+adminID, nil, tm.admin).Code)
		assert.Equal(t, "owner", tm.roleOf(t, adminID))

		// with a second owner the first may go
		decode[memberResponse](t, tm.setRole(tm.memberID, "owner", a.asAdmin, t), http.StatusOK)
		assert.Equal(t, http.StatusNoContent, a.request(t, http.MethodPost, leavePath, nil, a.asAdmin).Code)
		// and then the one who is left is the last
		assert.Equal(t, http.StatusConflict, a.request(t, http.MethodPost, leavePath, nil, tm.member).Code)
	})
}

// newTeamWithoutSecondOwner is a team whose only owner is the admin
func newTeamWithoutSecondOwner(t *testing.T, a *app) *team {
	t.Helper()
	tm := newTeam(t, a)
	decode[memberResponse](t, tm.setRole(tm.owner2ID, "member", a.asAdmin, t), http.StatusOK)
	return tm
}

func (a *app) ownerCount(t *testing.T, id workspace.ID) int {
	t.Helper()
	var n int
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		return db.NewSelect().Table("workspace_members").ColumnExpr("count(*)").Where("workspace_id = ?", string(id)).Where("role = 'owner'").Scan(context.Background(), &n)
	}))
	return n
}

func TestMembers_ARoleThatDoesNotExistOrAMemberWhoIsNotThereIsRefused(t *testing.T) {
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)

		assert.Equal(t, http.StatusBadRequest, tm.setRole(tm.memberID, "viewer", a.asAdmin, t).Code)
		assert.Equal(t, http.StatusBadRequest, tm.setRole(tm.memberID, "", a.asAdmin, t).Code)
		stranger := a.createUser(t, "stranger@example.com")
		for _, id := range []string{stranger, "00000000-0000-0000-0000-000000000000", "not-a-uuid"} {
			assert.Equal(t, http.StatusNotFound, tm.setRole(id, "admin", a.asAdmin, t).Code, id)
			assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodDelete, membersPath+"/"+id, nil, a.asAdmin).Code, id)
		}
	})
}

func TestMembers_AnAPIKeyActsAsAnAdminAndCanNotLeave(t *testing.T) {
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)

		decode[memberResponse](t, tm.setRole(tm.memberID, "admin", a.withAPIKey, t), http.StatusOK)
		assert.Equal(t, http.StatusForbidden, tm.setRole(tm.owner2ID, "member", a.withAPIKey, t).Code, "an admin's reach, never an owner's")
		assert.Equal(t, http.StatusForbidden, tm.setRole(tm.adminID, "owner", a.withAPIKey, t).Code)
		assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodPost, leavePath, nil, a.withAPIKey).Code, "a key has no user to leave")
		assert.Equal(t, 4, len(decode[[]memberResponse](t, a.request(t, http.MethodGet, membersPath, nil, a.asAdmin), http.StatusOK)))
	})
}

func TestMembers_ARemovedMemberLosesTheWorkspaceAtOnce(t *testing.T) {
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)
		require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/monitors", nil, tm.member).Code)

		w := a.request(t, http.MethodDelete, membersPath+"/"+tm.memberID, nil, tm.admin)
		require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

		// with no other workspace, every workspace route answers 404 with the same token
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, "/api/v1/monitors", nil, tm.member).Code)
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, membersPath, nil, tm.member, inWorkspace(tm.id)).Code)
		me := decode[meResponse](t, a.request(t, http.MethodGet, "/api/v1/me", nil, tm.member), http.StatusOK)
		assert.Empty(t, me.Workspaces)
		assert.Equal(t, "", tm.roleOf(t, tm.memberID))
	})
}

func TestMembers_AMemberWhoLeavesLosesTheWorkspaceAtOnceAndKeepsTheirOthers(t *testing.T) {
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)
		other := a.createWorkspace(t, "Other")
		a.addMember(t, other, tm.memberID, workspace.RoleOwner)

		w := a.request(t, http.MethodPost, leavePath, nil, tm.member, inWorkspace(tm.id))
		require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, "/api/v1/monitors", nil, tm.member, inWorkspace(tm.id)).Code)
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/monitors", nil, tm.member).Code, "the default is now the one that is left")
		me := decode[meResponse](t, a.request(t, http.MethodGet, "/api/v1/me", nil, tm.member), http.StatusOK)
		require.Len(t, me.Workspaces, 1)
		assert.Equal(t, "Other", me.Workspaces[0].Name)
		assert.Equal(t, "", tm.roleOf(t, tm.memberID))
	})
}

// publishedLeaves records the MemberLeft events and, as each is published, whether the membership
// it announces was already gone from the database
type publishedLeaves struct {
	mu     sync.Mutex
	events []workspace.MemberLeftEvent
	// goneWhenPublished is whether the member was gone when each event was published
	goneWhenPublished []bool
}

type recordingBus struct {
	events.EventBus
	db     func() *bun.DB
	leaves *publishedLeaves
}

func (b *recordingBus) PublishMessage(ctx context.Context, msg events.Message) error {
	if msg.Topic == workspace.MemberLeft.Name() {
		e := msg.Value.(workspace.MemberLeftEvent)
		var left int
		_ = b.db().NewSelect().Table("workspace_members").ColumnExpr("count(*)").
			Where("workspace_id = ?", string(e.WorkspaceID)).Where("user_id = ?", e.UserID).Scan(context.Background(), &left)
		b.leaves.mu.Lock()
		b.leaves.events = append(b.leaves.events, e)
		b.leaves.goneWhenPublished = append(b.leaves.goneWhenPublished, left == 0)
		b.leaves.mu.Unlock()
	}
	return b.EventBus.PublishMessage(ctx, msg)
}

func (l *publishedLeaves) snapshot() ([]workspace.MemberLeftEvent, []bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]workspace.MemberLeftEvent(nil), l.events...), append([]bool(nil), l.goneWhenPublished...)
}

func newAppRecordingLeaves(t *testing.T) (*app, *publishedLeaves) {
	t.Helper()
	leaves := &publishedLeaves{}
	var application *app
	a := newAppDecorated(t, func(c *dig.Container) error {
		return c.Decorate(func(bus events.EventBus, db *bun.DB) events.EventBus {
			return &recordingBus{EventBus: bus, db: func() *bun.DB { return db }, leaves: leaves}
		})
	})
	application = a
	application.registerAdmin(t)
	return application, leaves
}

// MemberLeft is published once, after the change is stored, for a removal and for a leave, and not
// at all for a change that is refused
func TestMembers_MemberLeftIsPublishedOnceAfterTheChangeIsStored(t *testing.T) {
	a, leaves := newAppRecordingLeaves(t)
	tm := newTeam(t, a)

	// refused: nothing is published
	require.Equal(t, http.StatusForbidden, a.request(t, http.MethodDelete, membersPath+"/"+tm.owner2ID, nil, tm.admin).Code)
	require.Equal(t, http.StatusForbidden, a.request(t, http.MethodDelete, membersPath+"/"+tm.adminID, nil, tm.member).Code)
	require.Equal(t, http.StatusNotFound, a.request(t, http.MethodDelete, membersPath+"/00000000-0000-0000-0000-000000000000", nil, a.asAdmin).Code)
	published, _ := leaves.snapshot()
	require.Empty(t, published, "a refused change announces nothing")
	require.Equal(t, http.StatusOK, a.request(t, http.MethodPatch, membersPath+"/"+tm.memberID, map[string]any{"role": "admin"}, a.asAdmin).Code)
	published, _ = leaves.snapshot()
	require.Empty(t, published, "a role change is not a departure")

	require.Equal(t, http.StatusNoContent, a.request(t, http.MethodDelete, membersPath+"/"+tm.memberID, nil, tm.admin).Code)
	published, gone := leaves.snapshot()
	assert.Equal(t, []workspace.MemberLeftEvent{{WorkspaceID: tm.id, UserID: tm.memberID}}, published)
	assert.Equal(t, []bool{true}, gone, "the membership was gone from the database when the event was published")

	require.Equal(t, http.StatusNoContent, a.request(t, http.MethodPost, leavePath, nil, tm.admin).Code)
	published, gone = leaves.snapshot()
	assert.Equal(t, []workspace.MemberLeftEvent{{WorkspaceID: tm.id, UserID: tm.memberID}, {WorkspaceID: tm.id, UserID: tm.adminID}}, published)
	assert.Equal(t, []bool{true, true}, gone)

	// the last owner's refused leave announces nothing either
	require.Equal(t, http.StatusNoContent, a.request(t, http.MethodDelete, membersPath+"/"+tm.owner2ID, nil, a.asAdmin).Code)
	before, _ := leaves.snapshot()
	require.Equal(t, http.StatusConflict, a.request(t, http.MethodPost, leavePath, nil, a.asAdmin).Code)
	after, _ := leaves.snapshot()
	assert.Equal(t, before, after)
}

// httpRecorder keeps the helper signatures short
type httpRecorder = httptest.ResponseRecorder

// Leaving takes no ID, so the isolation suite exempts it: what it can reach is the workspace the
// request names, and naming one the caller is not in answers 404 and changes nothing
func TestMembers_ANonMemberNamingThisWorkspaceCanNotLeaveChangeOrRemoveAnyone(t *testing.T) {
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)
		other := a.createWorkspace(t, "Other")
		bob := a.userIn(t, "bob@example.com", other, workspace.RoleOwner)
		inA := inWorkspace(tm.id)
		before := decode[[]memberResponse](t, a.request(t, http.MethodGet, membersPath, nil, a.asAdmin), http.StatusOK)

		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodPost, leavePath, nil, bob, inA).Code)
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodDelete, membersPath+"/"+tm.memberID, nil, bob, inA).Code)
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodPatch, membersPath+"/"+tm.memberID, map[string]any{"role": "owner"}, bob, inA).Code)
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodPost, invitesPath, map[string]any{"email": "x@example.com", "role": "member"}, bob, inA).Code)

		assert.Equal(t, before, decode[[]memberResponse](t, a.request(t, http.MethodGet, membersPath, nil, a.asAdmin), http.StatusOK))
		assert.Equal(t, 1, len(decode[[]memberResponse](t, a.request(t, http.MethodGet, membersPath, nil, bob), http.StatusOK)), "and Bob is still in his own")
	})
}

// Two owners who demote or remove each other at once never leave the workspace without one: each
// change reads the owners under a lock, so the second sees the first (or is refused as it is no
// longer an owner). Nothing answers 5xx, and PostgreSQL does not deadlock.
func TestMembers_TwoOwnersChangingEachOtherAtOnceNeverLeaveTheWorkspaceWithoutOne(t *testing.T) {
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)
		adminID := a.userID(t, adminEmail)
		changes := map[string]func(target string, as credentials) int{
			"demote": func(target string, as credentials) int { return tm.setRole(target, "member", as, t).Code },
			"remove": func(target string, as credentials) int {
				return a.request(t, http.MethodDelete, membersPath+"/"+target, nil, as).Code
			},
		}
		for name, change := range changes {
			var wg sync.WaitGroup
			codes := make([]int, 2)
			wg.Add(2)
			go func() { defer wg.Done(); codes[0] = change(tm.owner2ID, a.asAdmin) }()
			go func() { defer wg.Done(); codes[1] = change(adminID, tm.owner2) }()
			wg.Wait()

			assert.GreaterOrEqual(t, a.ownerCount(t, tm.id), 1, "%s: %v", name, codes)
			for _, code := range codes {
				assert.Less(t, code, 500, "%s: %v", name, codes)
			}
			// back to two owners for the next change
			for _, id := range []string{tm.owner2ID, adminID} {
				a.restoreOwner(t, tm.id, id)
			}
		}
	})
}

// restoreOwner makes the user an owner of the workspace, whether they are still a member or not
func (a *app) restoreOwner(t *testing.T, id workspace.ID, userID string) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		res, err := db.Exec("UPDATE workspace_members SET role = 'owner' WHERE workspace_id = ? AND user_id = ?", string(id), userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			_, err = db.Exec("INSERT INTO workspace_members (workspace_id, user_id, role, joined_at) VALUES (?, ?, 'owner', ?)", string(id), userID, time.Now().UTC())
		}
		return err
	}))
}
