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
