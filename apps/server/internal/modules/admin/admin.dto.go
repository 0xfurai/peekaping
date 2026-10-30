package admin

import (
	"fmt"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/limits"
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

// UpdateWorkspaceDto is the request to suspend or resume a workspace, or to set its own limits. It
// names at least one of the three.
type UpdateWorkspaceDto struct {
	// Suspended holds the workspace back (true) or lets it run again (false). A null reads as
	// missing, so it is not nullable.
	Suspended *bool `json:"suspended,omitempty" doc:"Suspend (true) or resume (false) the workspace." nullable:"false"`
	// Limits gives the workspace limits of its own (PP-160), which replace the install's defaults
	// whole: a limit left out, or 0, is none, so {} is no limits at all. A null reads as missing,
	// so it is not nullable: clearLimits removes them.
	Limits *WorkspaceLimitsDto `json:"limits,omitempty" doc:"Give the workspace limits of its own, which replace the install's defaults whole. A limit left out, or 0, is none, so {} is a workspace with no limits at all, whatever the defaults say. Not with clearLimits." nullable:"false"`
	// ClearLimits removes the workspace's own limits, so that the install's defaults apply again
	ClearLimits bool `json:"clearLimits,omitempty" doc:"true removes the workspace's own limits, so that the install's defaults apply to it again. Not with limits."`
}

// WorkspaceLimitsDto is a workspace's own limits, as an instance admin sets them. A field left out,
// or 0, is no limit.
type WorkspaceLimitsDto struct {
	Monitors         *int `json:"monitors,omitempty" minimum:"0" doc:"How many active monitors the workspace may have."`
	MinCheckInterval *int `json:"min_check_interval,omitempty" minimum:"0" doc:"The shortest check interval and retry interval of its monitors, in seconds. MIN_CHECK_INTERVAL applies as well: the stricter wins."`
	StatusPages      *int `json:"status_pages,omitempty" minimum:"0" doc:"How many status pages it may have."`
	CustomDomains    *int `json:"custom_domains,omitempty" minimum:"0" doc:"How many custom domains its status pages may have, together."`
	Members          *int `json:"members,omitempty" minimum:"0" doc:"How many members it may have, with its pending invites counted."`
	ProbesPerMonitor *int `json:"probes_per_monitor,omitempty" minimum:"0" doc:"How many probes one of its monitors may run on."`
	RetentionDays    *int `json:"retention_days,omitempty" minimum:"0" doc:"The longest data retention period it may keep, in days."`
	// The workspaces a user owns are counted against the install's defaults alone, so a workspace
	// has no owned_workspaces of its own
}

// limits are the workspace's own limits the request sets, or an error naming one below 0
func (d WorkspaceLimitsDto) limits() (limits.Limits, error) {
	l := limits.Limits{
		Monitors: d.Monitors, MinCheckInterval: d.MinCheckInterval, StatusPages: d.StatusPages,
		CustomDomains: d.CustomDomains, Members: d.Members,
		ProbesPerMonitor: d.ProbesPerMonitor, RetentionDays: d.RetentionDays,
	}
	for name, value := range map[string]*int{
		"monitors": d.Monitors, "min_check_interval": d.MinCheckInterval, "status_pages": d.StatusPages,
		"custom_domains": d.CustomDomains, "members": d.Members,
		"probes_per_monitor": d.ProbesPerMonitor, "retention_days": d.RetentionDays,
	} {
		if value != nil && *value < 0 {
			return limits.Limits{}, fmt.Errorf("the limit %s must be 0 or more", name)
		}
	}
	return l, nil
}

// LimitValuesResponse is every limit of a workspace, null for none
type LimitValuesResponse struct {
	Monitors         *int `json:"monitors" nullable:"true"`
	MinCheckInterval *int `json:"min_check_interval" nullable:"true"`
	StatusPages      *int `json:"status_pages" nullable:"true"`
	CustomDomains    *int `json:"custom_domains" nullable:"true"`
	Members          *int `json:"members" nullable:"true"`
	OwnedWorkspaces  *int `json:"owned_workspaces" nullable:"true" doc:"The install's default, which a user's workspaces are counted against, whatever the source."`
	ProbesPerMonitor *int `json:"probes_per_monitor" nullable:"true"`
	RetentionDays    *int `json:"retention_days" nullable:"true"`
}

// WorkspaceLimitsResponse is what a workspace is held to and where it comes from
type WorkspaceLimitsResponse struct {
	// Source is where the values come from: the workspace's own limits, the install's defaults or none
	Source limits.Origin       `json:"source" enum:"workspace,install,none"`
	Values LimitValuesResponse `json:"values"`
}

func workspaceLimitsResponse(l limits.Limits, source limits.Origin) WorkspaceLimitsResponse {
	return WorkspaceLimitsResponse{Source: source, Values: LimitValuesResponse{
		Monitors: l.Monitors, MinCheckInterval: l.MinCheckInterval, StatusPages: l.StatusPages,
		CustomDomains: l.CustomDomains, Members: l.Members, OwnedWorkspaces: l.OwnedWorkspaces,
		ProbesPerMonitor: l.ProbesPerMonitor, RetentionDays: l.RetentionDays,
	}}
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
	// SuspendedReason says why: "deleting" when an owner scheduled its deletion, else "admin" when an
	// instance admin suspended it, else "unverified" when it waits for its owner to verify their
	// email address. Null when the workspace runs.
	SuspendedReason *string `json:"suspendedReason" nullable:"true" enum:"deleting,admin,unverified"`
	// SuspendedAt is when the admin's or the verification's hold began, or null when the workspace
	// runs or is held by its scheduled deletion alone
	SuspendedAt *time.Time `json:"suspendedAt" nullable:"true"`
	// DeletionScheduledAt is when the workspace will be deleted, or null when no deletion is scheduled
	DeletionScheduledAt *time.Time `json:"deletionScheduledAt" nullable:"true"`
	// Limits are the limits the workspace is held to, and where they come from (PP-160)
	Limits WorkspaceLimitsResponse `json:"limits"`
}

func workspaceResponse(w *Workspace) *WorkspaceResponse {
	out := &WorkspaceResponse{
		ID: string(w.ID), Name: w.Name, CreatedAt: w.CreatedAt, Owners: append([]string{}, w.Owners...),
		MemberCount: w.MemberCount, Monitors: WorkspaceMonitorsResponse{Active: w.ActiveMonitors, Paused: w.PausedMonitors},
		StatusPages: w.StatusPages, CustomDomains: w.CustomDomains,
		Suspended: w.SuspendedReason != "", SuspendedAt: w.SuspendedAt, DeletionScheduledAt: w.DeletionScheduledAt,
		Limits: workspaceLimitsResponse(w.Limits, w.LimitsSource),
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
type DeleteWorkspaceInput = httpx.Input[httpx.NoBody, WorkspaceParams]
