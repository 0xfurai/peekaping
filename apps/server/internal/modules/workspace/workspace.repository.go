package workspace

import "context"

// Repository stores workspaces and their members. Every method joins the transaction its ctx
// carries (see internal/dbtx), so that a service can create a workspace and its first member
// together with the user.
type Repository interface {
	// Create adds a workspace with no member.
	Create(ctx context.Context, name string) (*Workspace, error)
	// CreateSuspended adds a workspace with no member that is suspended for the reason from the
	// start, so that there is no moment at which its monitors could run.
	CreateSuspended(ctx context.Context, name string, reason string) (*Workspace, error)
	// LiftUnverifiedOwnedBy ends the SuspendedUnverified suspension of every workspace the user
	// owns, and returns the workspaces it lifted: the caller wakes their monitors. A workspace
	// suspended for another reason, or one the user is only an admin or member of, stays as it is.
	// The lifted workspaces' monitors get a new updated_at, so that none counts as stale for the days
	// it was held back.
	LiftUnverifiedOwnedBy(ctx context.Context, userID string) ([]ID, error)
	// LiftUnverifiedWithVerifiedOwner ends the SuspendedUnverified suspension of the workspace if
	// one of its owners has a verified email, and returns it when it did (nil otherwise). It is for
	// a role change that makes a verified user an owner: no owner may be left to verify. Like
	// LiftUnverifiedOwnedBy, it gives the workspace's monitors a new updated_at.
	LiftUnverifiedWithVerifiedOwner(ctx context.Context, id ID) ([]ID, error)
	// Rename gives the workspace a new name and returns it, or nil when there is no workspace with
	// the ID. It changes no other workspace.
	Rename(ctx context.Context, id ID, name string) (*Workspace, error)
	// AddMember makes the user a member of the workspace with the role.
	AddMember(ctx context.Context, id ID, userID string, role Role) error
	// ListMemberships returns the user's places in workspaces, the one they joined first at the
	// front: that is their default workspace. The workspace ID breaks a tie.
	ListMemberships(ctx context.Context, userID string) ([]Membership, error)
	// FindByID returns the workspace, or nil when there is none with the ID.
	FindByID(ctx context.Context, id ID) (*Workspace, error)
	// FindOldest returns the workspace that was created first, or nil when the install has none.
	FindOldest(ctx context.Context) (*Workspace, error)
	// Delete removes the workspace and, by cascade, everything in it: its members, settings,
	// invites, API keys and records, and what hangs on those. It returns the monitors the workspace
	// held and the users who were its members, or nil when there is no workspace with the ID. On
	// PostgreSQL it holds the workspace's row first, so that a monitor or a member added at the same
	// time is not missed in what it returns: the insert waits, and is refused once the row is gone.
	Delete(ctx context.Context, id ID) (*Deleted, error)
	// ListIDsAnyWorkspace returns the ID of every workspace of the install, the oldest first. It
	// is for the background work that goes through the workspaces one by one, such as the cleanup
	// job.
	ListIDsAnyWorkspace(ctx context.Context) ([]ID, error)
}
