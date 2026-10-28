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
// its owner's email address is verified.
const SuspendedUnverified = "unverified"

// SuspendedByAdmin is the reason shown for a workspace that an instance admin suspended (PP-372).
// It is a hold of its own, stored as AdminSuspendedAt: it can be on beside the verification hold,
// and each ends on its own.
const SuspendedByAdmin = "admin"

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
	// SuspendedReason says why: SuspendedUnverified. Empty when the verification hold is off. The
	// admin's hold is AdminSuspendedAt, and ShownReason says which of the two to tell.
	SuspendedReason string `json:"suspendedReason,omitempty"`
	// AdminSuspendedAt is when an instance admin suspended the workspace, nil when no admin holds
	// it. It is independent of SuspendedAt: the workspace is suspended while either is set.
	AdminSuspendedAt *time.Time `json:"adminSuspendedAt,omitempty"`
}

// Suspended is whether the workspace's monitors are held back, by either hold
func (w Workspace) Suspended() bool { return w.SuspendedAt != nil || w.AdminSuspendedAt != nil }

// ShownReason is the reason to tell the workspace's members: SuspendedByAdmin when an instance
// admin holds it, else SuspendedUnverified, else empty when it runs.
func (w Workspace) ShownReason() string {
	switch {
	case w.AdminSuspendedAt != nil:
		return SuspendedByAdmin
	case w.SuspendedAt != nil:
		return w.SuspendedReason
	}
	return ""
}

// Membership is a user's place in a workspace.
type Membership struct {
	Workspace Workspace `json:"workspace"`
	Role      Role      `json:"role"`
	JoinedAt  time.Time `json:"joinedAt"`
}

// Deleted is what a deleted workspace held that the rest of the install is told about: its
// monitors, so that they stop, and its members, so that their open sockets leave its rooms.
type Deleted struct {
	MonitorIDs []string
	UserIDs    []string
}
