package organization

import (
	"time"
)

type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

type OrganizationUser struct {
	OrganizationID string        `json:"organization_id"`
	UserID         string        `json:"user_id"`
	Role           Role          `json:"role"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	User           *User         `json:"user,omitempty"`
	Organization   *Organization `json:"organization,omitempty"`
}

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type InvitationStatus string

const (
	InvitationStatusPending  InvitationStatus = "pending"
	InvitationStatusAccepted InvitationStatus = "accepted"
	InvitationStatusExpired  InvitationStatus = "expired"
)

type Invitation struct {
	ID             string           `json:"id"`
	OrganizationID string           `json:"organization_id"`
	Email          string           `json:"email"`
	Role           Role             `json:"role"`
	Token          string           `json:"token"`
	Status         InvitationStatus `json:"status"`
	CreatedAt      time.Time        `json:"created_at"`
	ExpiresAt      time.Time        `json:"expires_at"`
	Organization   *Organization    `json:"organization,omitempty"`
}
