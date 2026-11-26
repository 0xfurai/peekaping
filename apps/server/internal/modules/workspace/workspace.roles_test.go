package workspace_test

import (
	"fmt"
	"testing"

	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
)

func roleOf(r workspace.Role) *workspace.Role { return &r }

// The rules for who may change whom (docs/adr/0005 section 1): admins manage members and admins and
// not owners, owners manage everyone, and the last owner stays an owner.
func TestCanChange(t *testing.T) {
	owner, admin, member := workspace.RoleOwner, workspace.RoleAdmin, workspace.RoleMember
	remove := (*workspace.Role)(nil)

	tests := []struct {
		actor, target workspace.Role
		newRole       *workspace.Role
		owners        int
		want          error
	}{
		// a member changes nobody
		{member, member, roleOf(admin), 1, workspace.ErrForbidden},
		{member, member, remove, 1, workspace.ErrForbidden},
		{member, admin, remove, 1, workspace.ErrForbidden},

		// an admin manages members and admins
		{admin, member, roleOf(admin), 1, nil},
		{admin, admin, roleOf(member), 1, nil},
		{admin, member, remove, 1, nil},
		{admin, admin, remove, 1, nil},
		{admin, member, roleOf(member), 1, nil},
		// and does not touch owners, or make one
		{admin, owner, roleOf(admin), 2, workspace.ErrForbidden},
		{admin, owner, roleOf(member), 2, workspace.ErrForbidden},
		{admin, owner, remove, 2, workspace.ErrForbidden},
		{admin, member, roleOf(owner), 2, workspace.ErrForbidden},
		{admin, admin, roleOf(owner), 2, workspace.ErrForbidden},

		// an owner manages everyone
		{owner, member, roleOf(admin), 1, nil},
		{owner, member, roleOf(owner), 1, nil},
		{owner, admin, roleOf(owner), 1, nil},
		{owner, admin, remove, 1, nil},
		{owner, owner, roleOf(admin), 2, nil},
		{owner, owner, roleOf(member), 2, nil},
		{owner, owner, remove, 2, nil},

		// the last owner is not demoted or removed, by anyone
		{owner, owner, roleOf(admin), 1, workspace.ErrLastOwner},
		{owner, owner, roleOf(member), 1, workspace.ErrLastOwner},
		{owner, owner, remove, 1, workspace.ErrLastOwner},
		// and may stay one
		{owner, owner, roleOf(owner), 1, nil},

		// a role that does not exist is refused
		{owner, member, roleOf("viewer"), 1, workspace.ErrInvalidRole},
		{owner, member, roleOf(""), 1, workspace.ErrInvalidRole},
	}
	for _, tt := range tests {
		change := "removes"
		if tt.newRole != nil {
			change = fmt.Sprintf("makes %q", *tt.newRole)
		}
		t.Run(fmt.Sprintf("%s %s a %s with %d owners", tt.actor, change, tt.target, tt.owners), func(t *testing.T) {
			assert.ErrorIs(t, workspace.CanChange(tt.actor, tt.target, tt.newRole, tt.owners), tt.want)
		})
	}
}

// Anyone may leave, except the last owner
func TestCanLeave(t *testing.T) {
	assert.NoError(t, workspace.CanLeave(workspace.RoleMember, 1))
	assert.NoError(t, workspace.CanLeave(workspace.RoleAdmin, 1))
	assert.NoError(t, workspace.CanLeave(workspace.RoleOwner, 2))
	assert.ErrorIs(t, workspace.CanLeave(workspace.RoleOwner, 1), workspace.ErrLastOwner)
}

// Only an owner invites an owner, and an admin invites members and admins
func TestCanInvite(t *testing.T) {
	assert.NoError(t, workspace.CanInvite(workspace.RoleAdmin, workspace.RoleMember))
	assert.NoError(t, workspace.CanInvite(workspace.RoleAdmin, workspace.RoleAdmin))
	assert.ErrorIs(t, workspace.CanInvite(workspace.RoleAdmin, workspace.RoleOwner), workspace.ErrForbidden)
	assert.NoError(t, workspace.CanInvite(workspace.RoleOwner, workspace.RoleOwner))
	assert.ErrorIs(t, workspace.CanInvite(workspace.RoleMember, workspace.RoleMember), workspace.ErrForbidden)
	assert.ErrorIs(t, workspace.CanInvite(workspace.RoleOwner, "viewer"), workspace.ErrInvalidRole)
}
