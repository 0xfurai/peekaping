// Package admin is the admin area of the install (docs/adr/0005 section 10): what an instance admin
// does to the users and the workspaces of the install. Its routes are install routes: they name no workspace, and the
// instance admin flag gives no access to any workspace's data.
package admin

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"peekaping/internal/limits"
	"peekaping/internal/modules/workspace"
)

var (
	// ErrUserNotFound is for a user that does not exist. It answers 404.
	ErrUserNotFound = errors.New("user not found")
	// ErrSelf is for an admin who tries to disable or delete their own account. It answers 409.
	ErrSelf = errors.New("you can't do this to your own account")
	// ErrLastInstanceAdmin is for a change that would leave the install with no instance admin who can sign in (one with the flag and an active account). It
	// answers 409.
	ErrLastInstanceAdmin = errors.New("the install needs at least one instance admin")
	// ErrWorkspaceNotFound is for a workspace that does not exist. It answers 404.
	ErrWorkspaceNotFound = errors.New("workspace not found")
	// ErrLastOwner is for a deletion of a user who is the last owner of a workspace. The error that
	// wraps it, LastOwnerError, names the workspaces. It answers 409.
	ErrLastOwner = errors.New("the user is the last owner of a workspace")
)

// LastOwnerError is the ErrLastOwner of a user who is the only owner of the Workspaces
type LastOwnerError struct {
	Workspaces []WorkspaceRef
}

func (e *LastOwnerError) Error() string {
	names := make([]string, len(e.Workspaces))
	for i, w := range e.Workspaces {
		names[i] = fmt.Sprintf("%q", w.Name)
	}
	return fmt.Sprintf("%s: %s", ErrLastOwner, strings.Join(names, ", "))
}

func (e *LastOwnerError) Is(target error) bool { return target == ErrLastOwner }

// WorkspaceRef names a workspace
type WorkspaceRef struct {
	ID   workspace.ID
	Name string
}

// Membership is a user's place in a workspace, with the workspace's name
type Membership struct {
	WorkspaceID   workspace.ID
	WorkspaceName string
	Role          workspace.Role
}

// User is a user as the admin area shows them. It holds nothing secret.
type User struct {
	ID    string
	Email string
	// Active is false for a disabled user, who can't sign in
	Active        bool
	InstanceAdmin bool
	// TwoFactor is whether the user has 2FA on
	TwoFactor     bool
	EmailVerified bool
	CreatedAt     time.Time
	// LastLoginAt is nil for a user who has not signed in since it was recorded
	LastLoginAt *time.Time
	// Memberships are the user's workspaces, the one they joined first at the front
	Memberships []Membership
}

// UserUpdate is what an instance admin changes about a user: nil fields stay as they are
type UserUpdate struct {
	InstanceAdmin *bool
	Active        *bool
}

// ResetLink is a single-use password reset link, shown once
type ResetLink struct {
	Link      string
	ExpiresAt time.Time
}

// Workspace is a workspace as the admin area shows it: counts and metadata, never what is inside. It
// holds no monitor's target, no channel and no status page's content.
type Workspace struct {
	ID        workspace.ID
	Name      string
	CreatedAt time.Time
	// Owners are the emails of the workspace's owners, in order
	Owners []string
	// MemberCount counts every member, owners included
	MemberCount    int
	ActiveMonitors int
	PausedMonitors int
	StatusPages    int
	// CustomDomains counts the domains of all the workspace's status pages
	CustomDomains int
	// SuspendedAt is when the hold the reason names began: the admin's when an admin holds the
	// workspace, else the verification's. Nil when the workspace runs.
	SuspendedAt *time.Time
	// SuspendedReason is workspace.SuspendedByAdmin, workspace.SuspendedUnverified, or empty
	SuspendedReason string
	// Limits are what the workspace is held to (PP-160), and LimitsSource where they come from. The
	// service fills them in: the repository reads none.
	Limits       limits.Limits
	LimitsSource limits.Origin
}

// The names of the admin actions, as the log says them
const (
	ActionSetInstanceAdmin = "set_instance_admin"
	ActionDisableUser      = "disable_user"
	ActionEnableUser       = "enable_user"
	ActionCreateResetLink  = "create_reset_link"
	ActionDisableTwoFA     = "disable_2fa"
	ActionDeleteUser       = "delete_user"

	ActionSuspendWorkspace     = "suspend_workspace"
	ActionResumeWorkspace      = "resume_workspace"
	ActionDeleteWorkspace      = "delete_workspace"
	ActionSetWorkspaceLimits   = "set_workspace_limits"
	ActionClearWorkspaceLimits = "clear_workspace_limits"
)
