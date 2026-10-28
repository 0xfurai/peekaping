package workspace

import (
	"errors"
	"fmt"
	"time"
)

// InviteLifetime is how long an invite works after it is made
const InviteLifetime = 7 * 24 * time.Hour

// InviteTokenPrefix starts the token of an invite, so that a leaked one is recognised for what it is
const InviteTokenPrefix = "pki_"

var (
	// ErrInviteNotFound is for an invite that does not exist: an unknown token, or an ID that no
	// pending invite of the workspace has. It answers 404.
	ErrInviteNotFound = errors.New("invite not found")
	// ErrInviteUnavailable is for an invite that exists and can't be used any more. It answers 410.
	ErrInviteUnavailable = errors.New("this invite can't be used")
	// ErrInviteUsed, ErrInviteExpired and ErrInviteRevoked say why, each an ErrInviteUnavailable.
	ErrInviteUsed    = fmt.Errorf("%w: it was already used", ErrInviteUnavailable)
	ErrInviteExpired = fmt.Errorf("%w: it has expired", ErrInviteUnavailable)
	ErrInviteRevoked = fmt.Errorf("%w: it was revoked", ErrInviteUnavailable)
	// ErrInviteEmailMismatch is for an account whose email is not the one the invite names. It
	// answers 403.
	ErrInviteEmailMismatch = errors.New("this invite is for another email address")
	// ErrAlreadyMember is for an invite to, or the acceptance of one by, someone who is a member of
	// the workspace already. It answers 409.
	ErrAlreadyMember = errors.New("already a member of this workspace")
	// ErrMemberNotFound is for a user who is not a member of the workspace. It answers 404.
	ErrMemberNotFound = errors.New("member not found")
)

// InviteStatus says where an invite stands
type InviteStatus string

const (
	InvitePending  InviteStatus = "pending"
	InviteAccepted InviteStatus = "accepted"
	InviteRevoked  InviteStatus = "revoked"
	InviteExpired  InviteStatus = "expired"
)

// Member is a user's place in a workspace, as the workspace's people see it
type Member struct {
	UserID   string    `json:"userId"`
	Email    string    `json:"email"`
	Role     Role      `json:"role"`
	JoinedAt time.Time `json:"joinedAt"`
}

// Invite is an invitation to a workspace. The token is not part of it: the invite stores a hash.
type Invite struct {
	ID            string
	WorkspaceID   ID
	WorkspaceName string
	// Email is lower-cased and trimmed
	Email     string
	Role      Role
	InvitedBy *string
	CreatedAt time.Time
	ExpiresAt time.Time

	AcceptedAt *time.Time
	AcceptedBy *string
	RevokedAt  *time.Time
}

// StatusAt is the invite's status at the time: an invite that was used or revoked stays so, and
// one that was neither is pending until it expires.
func (i Invite) StatusAt(now time.Time) InviteStatus {
	switch {
	case i.AcceptedAt != nil:
		return InviteAccepted
	case i.RevokedAt != nil:
		return InviteRevoked
	case !now.Before(i.ExpiresAt):
		return InviteExpired
	}
	return InvitePending
}

// CreatedInvite is an invite just made, with the token that opens it. The token is shown once.
type CreatedInvite struct {
	Invite
	Token string
	// Link is the address that opens the invite in the web app
	Link string
}

// InvitePreview is what anyone who holds an invite's token may see of it
type InvitePreview struct {
	WorkspaceName string
	Email         string
	Role          Role
	Status        InviteStatus
}
