package workspacehttp

import (
	"strings"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/workspace"
)

// ChangeRoleDto is the request to change a member's role
type ChangeRoleDto struct {
	Role string `json:"role" validate:"required,oneof=owner admin member"`
}

// CreateInviteDto is the request to invite someone
type CreateInviteDto struct {
	Email string `json:"email" validate:"required,email,max=255"`
	Role  string `json:"role" validate:"required,oneof=owner admin member"`
}

// normalize trims the spaces a pasted email may carry
func (d *CreateInviteDto) normalize() { d.Email = strings.TrimSpace(d.Email) }

// CreateWorkspaceDto is the request to make a workspace
type CreateWorkspaceDto struct {
	Name string `json:"name" validate:"required,min=1,max=255"`
}

// normalize trims the name, so that one of only spaces is refused as empty and the trimmed name is stored
func (d *CreateWorkspaceDto) normalize() { d.Name = strings.TrimSpace(d.Name) }

// RenameWorkspaceDto is the request to rename the workspace
type RenameWorkspaceDto struct {
	Name string `json:"name" validate:"required,min=1,max=255"`
}

// normalize trims the name, so that one of only spaces is refused as empty
func (d *RenameWorkspaceDto) normalize() { d.Name = strings.TrimSpace(d.Name) }

// ScheduleDeletionDto is the request to schedule the deletion of the workspace. The name is not
// trimmed or lower-cased: the owner types it exactly as it is.
type ScheduleDeletionDto struct {
	Name string `json:"name"`
}

// ScheduledDeletionResponse says when the workspace will be deleted
type ScheduledDeletionResponse struct {
	// DeletionScheduledAt is when the workspace and everything in it will be deleted, 7 days after the
	// owner scheduled it
	DeletionScheduledAt time.Time `json:"deletionScheduledAt"`
}

// NameMismatchResponse is the 400 for a deletion scheduled with a name that is not the workspace's
type NameMismatchResponse struct {
	Message string                `json:"message" example:"The name typed is not the workspace's name"`
	Data    httpx.Null            `json:"data"`
	Error   NameMismatchErrorBody `json:"error"`
}

// NameMismatchErrorBody holds the code of a NameMismatchResponse
type NameMismatchErrorBody struct {
	Code string `json:"code" example:"WORKSPACE_NAME_MISMATCH"`
}

// MemberResponse is a member of the workspace
type MemberResponse struct {
	UserID   string    `json:"userId"`
	Email    string    `json:"email"`
	Role     string    `json:"role" enum:"owner,admin,member"`
	JoinedAt time.Time `json:"joinedAt"`
}

func memberResponse(m workspace.Member) *MemberResponse {
	return &MemberResponse{UserID: m.UserID, Email: m.Email, Role: string(m.Role), JoinedAt: m.JoinedAt}
}

// InviteResponse is an invite to the workspace. It holds no token: that is shown once, when the
// invite is made.
type InviteResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role" enum:"owner,admin,member"`
	// Status is pending, or expired once the invite's 7 days are over
	Status string `json:"status" enum:"pending,expired"`
	// InvitedBy is the user who made the invite, or null for an API key or a user who is gone
	InvitedBy *string   `json:"invitedBy"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func inviteResponse(i workspace.Invite, now time.Time) InviteResponse {
	return InviteResponse{
		ID: i.ID, Email: i.Email, Role: string(i.Role), Status: string(i.StatusAt(now)),
		InvitedBy: i.InvitedBy, CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt,
	}
}

// CreatedInviteResponse is an invite just made, with the link that opens it. The link and the token
// are shown this once: the invite stores a hash.
type CreatedInviteResponse struct {
	InviteResponse
	// Token is the invite's secret, which the link holds
	Token string `json:"token"`
	// Link is the address that opens the invite in the web app. Nothing is emailed: the inviter
	// copies it to the person they invite.
	Link string `json:"link"`
}

// InvitePreviewResponse is what anyone who holds the invite's token may see of it
type InvitePreviewResponse struct {
	WorkspaceName string `json:"workspaceName"`
	Email         string `json:"email"`
	Role          string `json:"role" enum:"owner,admin,member"`
	// Valid is whether the invite can be used now
	Valid bool `json:"valid"`
	// Status is pending, or why the invite can't be used: accepted, revoked or expired
	Status string `json:"status" enum:"pending,accepted,revoked,expired"`
}

// WorkspaceResponse is a workspace and the role of the user who made it or joined it
type WorkspaceResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Role is what the user may do there: owner, admin or member
	Role string `json:"role" enum:"owner,admin,member"`
}

func workspaceResponse(m *workspace.Membership) *WorkspaceResponse {
	return &WorkspaceResponse{ID: string(m.Workspace.ID), Name: m.Workspace.Name, Role: string(m.Role)}
}

type ListMembersInput = httpx.Input[httpx.NoBody, struct{}]
type GetLimitsInput = httpx.Input[httpx.NoBody, struct{}]

type MemberParams struct {
	UserID string `path:"userId" doc:"The member's user ID" required:"true"`
}
type ChangeRoleInput = httpx.Input[ChangeRoleDto, MemberParams]
type RemoveMemberInput = httpx.Input[httpx.NoBody, MemberParams]

type LeaveInput = httpx.Input[httpx.NoBody, struct{}]

type ScheduleDeletionInput = httpx.Input[ScheduleDeletionDto, struct{}]
type CancelDeletionInput = httpx.Input[httpx.NoBody, struct{}]

type ListInvitesInput = httpx.Input[httpx.NoBody, struct{}]
type CreateInviteInput = httpx.Input[CreateInviteDto, struct{}]

type InviteParams struct {
	ID string `path:"id" doc:"The invite's ID" required:"true"`
}
type RevokeInviteInput = httpx.Input[httpx.NoBody, InviteParams]

type InviteTokenParams struct {
	Token string `path:"token" doc:"The invite's token, from its link" required:"true"`
}
type PreviewInviteInput = httpx.Input[httpx.NoBody, InviteTokenParams]
type AcceptInviteInput = httpx.Input[httpx.NoBody, InviteTokenParams]

type CreateWorkspaceInput = httpx.Input[CreateWorkspaceDto, struct{}]
type RenameWorkspaceInput = httpx.Input[RenameWorkspaceDto, struct{}]
