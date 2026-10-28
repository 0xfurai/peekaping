package admin

import (
	"time"

	"peekaping/internal/httpx"
)

// UpdateUserDto is the request to change a user. A field left out stays as it is, and at least one
// is needed.
type UpdateUserDto struct {
	// InstanceAdmin gives (true) or takes (false) the instance admin flag. The last instance admin
	// can't lose it (409).
	InstanceAdmin *bool `json:"instanceAdmin,omitempty" doc:"Give or take the instance admin flag. The last instance admin can't lose it."`
	// Active disables (false) or enables (true) the user. A disabled user can't sign in and their
	// sessions end; their workspaces keep running. An admin can't disable themselves (409).
	Active *bool `json:"active,omitempty" doc:"Disable (false) or enable (true) the user. A disabled user can't sign in and their sessions end; their workspaces keep running."`
}

// UserWorkspaceResponse is a user's place in a workspace
type UserWorkspaceResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Role is what the user may do there: owner, admin or member
	Role string `json:"role" enum:"owner,admin,member"`
}

// LastOwnerErrorResponse is the 409 for deleting a user who is the last owner of a workspace. Like
// a push token that another monitor holds, it carries a code, and it lists the workspaces so that a
// client does not read their names out of a message.
type LastOwnerErrorResponse struct {
	Error LastOwnerErrorBody `json:"error"`
}

// LastOwnerErrorBody is the code of a LastOwnerErrorResponse and the workspaces the user is the
// only owner of
type LastOwnerErrorBody struct {
	Code       string               `json:"code" example:"LAST_OWNER"`
	Workspaces []LastOwnerWorkspace `json:"workspaces"`
}

// LastOwnerWorkspace is a workspace the user is the last owner of
type LastOwnerWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// UserResponse is a user of the install, as the admin area shows them
type UserResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	// InstanceAdmin is whether the user manages what belongs to the install and not to a workspace
	InstanceAdmin bool `json:"instanceAdmin"`
	// Active is false for a disabled user, who can't sign in
	Active bool `json:"active"`
	// TwoFactorEnabled is whether the user has 2FA on
	TwoFactorEnabled bool `json:"twoFactorEnabled"`
	// EmailVerified is whether the user followed the link in the verification email. Accounts that
	// existed before account emails count as verified.
	EmailVerified bool      `json:"emailVerified"`
	CreatedAt     time.Time `json:"createdAt"`
	// LastLoginAt is when the user last signed in, or null when they have not since it was recorded
	LastLoginAt *time.Time `json:"lastLoginAt" nullable:"true"`
	// Workspaces are the user's places, the one they joined first at the front. Never null.
	Workspaces []UserWorkspaceResponse `json:"workspaces" nullable:"false"`
}

func userResponse(u *User) *UserResponse {
	workspaces := make([]UserWorkspaceResponse, len(u.Memberships))
	for i, m := range u.Memberships {
		workspaces[i] = UserWorkspaceResponse{ID: string(m.WorkspaceID), Name: m.WorkspaceName, Role: string(m.Role)}
	}
	return &UserResponse{
		ID: u.ID, Email: u.Email, InstanceAdmin: u.InstanceAdmin, Active: u.Active,
		TwoFactorEnabled: u.TwoFactor, EmailVerified: u.EmailVerified,
		CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt, Workspaces: workspaces,
	}
}

// UsersPageResponse is a page of users
type UsersPageResponse struct {
	Users []*UserResponse `json:"users" nullable:"false"`
	// Total is how many users match the search, on all pages
	Total int `json:"total"`
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

// ResetLinkResponse is a password reset link just made. The link is shown this once: the server
// stores a hash of its token.
type ResetLinkResponse struct {
	// Link opens the web app's reset page. It works once and ends the user's sessions when used.
	Link      string    `json:"link"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type ListUsersParams struct {
	Q     string `query:"q" doc:"Only users whose email contains this text, ignoring case"`
	Page  int    `query:"page" doc:"Page number, from 0" default:"0" minimum:"0"`
	Limit int    `query:"limit" doc:"Users per page" default:"20" minimum:"1" maximum:"100"`
}
type ListUsersInput = httpx.Input[httpx.NoBody, ListUsersParams]

type UserParams struct {
	ID string `path:"id" doc:"The user's ID" required:"true"`
}
type UpdateUserInput = httpx.Input[UpdateUserDto, UserParams]
type CreateResetLinkInput = httpx.Input[httpx.NoBody, UserParams]
type DisableTwoFAInput = httpx.Input[httpx.NoBody, UserParams]
type DeleteUserInput = httpx.Input[httpx.NoBody, UserParams]

// UpdateWorkspaceDto is the request to suspend or resume a workspace
type UpdateWorkspaceDto struct {
	// Suspended holds the workspace back (true) or lets it run again (false). It is required.
	Suspended *bool `json:"suspended,omitempty" doc:"Suspend (true) or resume (false) the workspace. Required." required:"true"`
}

// WorkspaceMonitorsResponse counts a workspace's monitors by whether they run
type WorkspaceMonitorsResponse struct {
	Active int `json:"active"`
	Paused int `json:"paused"`
}

// WorkspaceResponse is a workspace of the install, as the admin area shows it: counts and metadata,
// never a monitor, a channel or anything else inside it
type WorkspaceResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	// Owners are the emails of the workspace's owners, in order. Never null.
	Owners []string `json:"owners" nullable:"false"`
	// MemberCount counts every member, owners included
	MemberCount int                       `json:"memberCount"`
	Monitors    WorkspaceMonitorsResponse `json:"monitors"`
	StatusPages int                       `json:"statusPages"`
	// CustomDomains counts the domains of all the workspace's status pages
	CustomDomains int `json:"customDomains"`
	// Suspended is whether the workspace's monitors are held back, whichever hold it is under
	Suspended bool `json:"suspended"`
	// SuspendedReason says why: "admin" when an instance admin suspended it, else "unverified" when
	// it waits for its owner to verify their email address. Null when the workspace runs.
	SuspendedReason *string `json:"suspendedReason" nullable:"true" enum:"admin,unverified"`
	// SuspendedAt is when that hold began, or null when the workspace runs
	SuspendedAt *time.Time `json:"suspendedAt" nullable:"true"`
}

func workspaceResponse(w *Workspace) *WorkspaceResponse {
	out := &WorkspaceResponse{
		ID: string(w.ID), Name: w.Name, CreatedAt: w.CreatedAt, Owners: append([]string{}, w.Owners...),
		MemberCount: w.MemberCount, Monitors: WorkspaceMonitorsResponse{Active: w.ActiveMonitors, Paused: w.PausedMonitors},
		StatusPages: w.StatusPages, CustomDomains: w.CustomDomains,
		Suspended: w.SuspendedAt != nil, SuspendedAt: w.SuspendedAt,
	}
	if w.SuspendedReason != "" {
		reason := w.SuspendedReason
		out.SuspendedReason = &reason
	}
	return out
}

// WorkspacesPageResponse is a page of workspaces
type WorkspacesPageResponse struct {
	Workspaces []*WorkspaceResponse `json:"workspaces" nullable:"false"`
	// Total is how many workspaces match the search, on all pages
	Total int `json:"total"`
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

type ListWorkspacesParams struct {
	Q     string `query:"q" doc:"Only workspaces whose name or whose owner's email contains this text, ignoring case"`
	Page  int    `query:"page" doc:"Page number, from 0" default:"0" minimum:"0"`
	Limit int    `query:"limit" doc:"Workspaces per page" default:"20" minimum:"1" maximum:"100"`
}
type ListWorkspacesInput = httpx.Input[httpx.NoBody, ListWorkspacesParams]

type WorkspaceParams struct {
	ID string `path:"id" doc:"The workspace's ID" required:"true"`
}
type UpdateWorkspaceInput = httpx.Input[UpdateWorkspaceDto, WorkspaceParams]
