// Package workspace holds workspaces and their members (docs/adr/0005-workspaces-and-data-isolation.md).
// It imports no other module, so that every module can import it.
package workspace

import (
	"time"

	"peekaping/internal/modules/workspace/workspaceid"
)

// ID is the ID of a workspace. It is a type of its own so that it can't be swapped with the string
// ID of a record that a workspace owns. It is defined in a package of its own that the probe may
// link (workspaceid), and this is the same type.
type ID = workspaceid.ID

// Role is what a member may do in their workspace.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// SuspendedUnverified is the reason of a workspace that an open signup made (PP-155): it runs once
// its owner's email address is verified. The reason of an admin's suspension will be added by PP-372.
const SuspendedUnverified = "unverified"

// Workspace owns monitors and everything around them.
type Workspace struct {
	ID        ID        `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// SuspendedAt is when the workspace was suspended, nil when it runs. A suspended workspace keeps
	// its monitors and shows them, but none of them runs: no checks, no missed-push detection, no
	// notifications.
	SuspendedAt *time.Time `json:"suspendedAt,omitempty"`
	// SuspendedReason says why: SuspendedUnverified, for now. Empty when the workspace runs.
	SuspendedReason string `json:"suspendedReason,omitempty"`
}

// Suspended is whether the workspace's monitors are held back
func (w Workspace) Suspended() bool { return w.SuspendedAt != nil }

// Membership is a user's place in a workspace.
type Membership struct {
	Workspace Workspace `json:"workspace"`
	Role      Role      `json:"role"`
	JoinedAt  time.Time `json:"joinedAt"`
}
