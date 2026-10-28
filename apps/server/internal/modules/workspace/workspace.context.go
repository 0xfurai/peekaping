package workspace

import (
	"context"
	"errors"
)

var (
	// ErrNotFound is for a request that runs in no workspace: the user is in none, or the one the
	// request names isn't theirs. It answers 404, as if the workspace didn't exist.
	ErrNotFound = errors.New("workspace not found")
	// ErrForbidden is for a member whose role doesn't allow what they ask. It answers 403.
	ErrForbidden = errors.New("your role in the workspace doesn't allow this")
)

type contextKey struct{}

type requestWorkspace struct {
	id   ID
	role Role
}

// WithContext returns a context for a request that runs in the workspace, with the role of its
// caller there. The auth middleware sets it (docs/adr/0005 section 7).
func WithContext(ctx context.Context, id ID, role Role) context.Context {
	return context.WithValue(ctx, contextKey{}, requestWorkspace{id: id, role: role})
}

// FromContext returns the workspace a request runs in and its caller's role there. ok is false
// for a context that has none.
func FromContext(ctx context.Context) (id ID, role Role, ok bool) {
	w, ok := ctx.Value(contextKey{}).(requestWorkspace)
	return w.id, w.role, ok
}

// rank orders the roles: a role may do whatever the ones before it may. A role not listed holds
// nothing.
var rank = map[Role]int{RoleMember: 1, RoleAdmin: 2, RoleOwner: 3}

// RequireRole is nil when the caller of the request holds min or a role above it: member, then
// admin, then owner. It returns ErrNotFound for a context with no workspace and ErrForbidden for
// a role that is too low.
func RequireRole(ctx context.Context, min Role) error {
	_, role, ok := FromContext(ctx)
	if !ok {
		return ErrNotFound
	}
	need, known := rank[min]
	if !known || rank[role] < need {
		return ErrForbidden
	}
	return nil
}

// RequireInvite is nil when the caller of the request may invite someone with the role to its
// workspace: an admin or an owner, and only an owner invites an owner (CanInvite). It returns
// ErrNotFound for a context with no workspace, ErrForbidden or ErrInvalidRole.
func RequireInvite(ctx context.Context, role Role) error {
	if err := RequireRole(ctx, RoleAdmin); err != nil {
		return err
	}
	_, actor, _ := FromContext(ctx)
	return CanInvite(actor, role)
}
