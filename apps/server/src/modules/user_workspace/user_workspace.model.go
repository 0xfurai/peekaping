package user_workspace

import (
	"time"
)

type Model struct {
	UserID      string    `json:"user_id"`
	WorkspaceID string    `json:"workspace_id"`
	Role        string    `json:"role"` // owner, admin, member, viewer, etc.
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type UpdateModel struct {
	Role *string `json:"role"`
}
