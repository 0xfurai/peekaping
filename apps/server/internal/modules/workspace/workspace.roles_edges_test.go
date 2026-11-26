package workspace_test

import (
	"fmt"
	"testing"

	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
)

// The order in which CanChange refuses: an invalid role first, then an actor who changes nobody,
// then an owner's change that is not the owner's to make, then the last owner.
func TestCanChange_RefusesInOrder(t *testing.T) {
	owner, admin, member := workspace.RoleOwner, workspace.RoleAdmin, workspace.RoleMember

	tests := []struct {
		name          string
		actor, target workspace.Role
		newRole       *workspace.Role
		owners        int
		want          error
	}{
		{"an invalid role beats a member's refusal", member, member, roleOf("viewer"), 1, workspace.ErrInvalidRole},
		{"an invalid role beats an admin touching an owner", admin, owner, roleOf("viewer"), 1, workspace.ErrInvalidRole},
		{"an invalid role beats the last owner", owner, owner, roleOf("viewer"), 1, workspace.ErrInvalidRole},
		{"a made-up actor changes nobody", "viewer", member, roleOf(admin), 1, workspace.ErrForbidden},
		{"an actor with no role changes nobody", "", member, nil, 1, workspace.ErrForbidden},
		{"an admin touching the last owner is forbidden, not the last owner", admin, owner, roleOf(member), 1, workspace.ErrForbidden},
		{"an admin removing the last owner is forbidden", admin, owner, nil, 1, workspace.ErrForbidden},
		{"a member touching the last owner is forbidden", member, owner, nil, 1, workspace.ErrForbidden},
		{"an owner removing an owner of a workspace with none counted", owner, owner, nil, 0, workspace.ErrLastOwner},
		{"an owner keeping an owner of a workspace with none counted", owner, owner, roleOf(owner), 0, nil},
		{"an owner making a member an owner of a workspace with one", owner, member, roleOf(owner), 1, nil},
		{"an admin making a member an admin", admin, member, roleOf(admin), 0, nil},
		{"an owner removing a member of a workspace with one owner", owner, member, nil, 1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorIs(t, workspace.CanChange(tt.actor, tt.target, tt.newRole, tt.owners), tt.want)
			if tt.want == nil {
				assert.NoError(t, workspace.CanChange(tt.actor, tt.target, tt.newRole, tt.owners))
			}
		})
	}
}

func TestValidRole(t *testing.T) {
	for _, r := range []workspace.Role{workspace.RoleOwner, workspace.RoleAdmin, workspace.RoleMember} {
		assert.True(t, workspace.ValidRole(r), fmt.Sprint(r))
	}
	for _, r := range []workspace.Role{"", "viewer", "OWNER"} {
		assert.False(t, workspace.ValidRole(r), fmt.Sprint(r))
	}
}
