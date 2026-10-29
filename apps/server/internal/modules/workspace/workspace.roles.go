package workspace

import "errors"

var (
	// ErrLastOwner is for a change that would leave a workspace with no owner. It answers 409.
	ErrLastOwner = errors.New("a workspace needs at least one owner")
	// ErrInvalidRole is for a role that is not owner, admin or member. It answers 400.
	ErrInvalidRole = errors.New("the role must be owner, admin or member")
)

// ValidRole is whether r is one of the three roles
func ValidRole(r Role) bool {
	_, ok := rank[r]
	return ok
}

// CanChange decides whether a member holding the actor role may give a member with the target role
// the new role, or remove them when newRole is nil (docs/adr/0005 section 1). owners is how many
// owners the workspace has now. It returns nil, ErrForbidden, ErrInvalidRole or ErrLastOwner.
//
// An API key acts as an admin, so it passes RoleAdmin. A member changes nobody; an admin manages
// members and admins, never an owner, and makes no owner; an owner manages everyone. Nobody
// demotes or removes the last owner, an owner included.
func CanChange(actor, target Role, newRole *Role, owners int) error {
	if newRole != nil && !ValidRole(*newRole) {
		return ErrInvalidRole
	}
	if rank[actor] < rank[RoleAdmin] {
		return ErrForbidden
	}
	touchesOwner := target == RoleOwner || (newRole != nil && *newRole == RoleOwner)
	if touchesOwner && actor != RoleOwner {
		return ErrForbidden
	}
	if target == RoleOwner && (newRole == nil || *newRole != RoleOwner) && owners <= 1 {
		return ErrLastOwner
	}
	return nil
}

// CanLeave decides whether a member with the role may leave their workspace: anyone, unless they
// are its last owner. owners is how many owners the workspace has now.
func CanLeave(role Role, owners int) error {
	if role == RoleOwner && owners <= 1 {
		return ErrLastOwner
	}
	return nil
}

// CanInvite decides whether a member holding the actor role may invite someone with the role: an
// admin or an owner, and only an owner invites an owner.
func CanInvite(actor, invited Role) error {
	if !ValidRole(invited) {
		return ErrInvalidRole
	}
	if rank[actor] < rank[RoleAdmin] || (invited == RoleOwner && actor != RoleOwner) {
		return ErrForbidden
	}
	return nil
}
