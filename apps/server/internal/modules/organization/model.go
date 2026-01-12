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
