package notification_channel

import (
	"errors"
	"time"

	"peekaping/internal/modules/workspace/workspaceid"
)

// ErrNotFound is a channel that isn't in the workspace of the request: it doesn't exist, or it
// belongs to another workspace, and the caller can't tell which (docs/adr/0005 section 8)
var ErrNotFound = errors.New("notification channel not found")

type Model struct {
	ID string `json:"id"`
	// WorkspaceID is the workspace the channel belongs to. It is stored with the channel and read
	// back with it, never taken from a request body, and it is not part of the API's answer.
	WorkspaceID workspaceid.ID `json:"-"`
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Active      bool           `json:"active"`
	IsDefault   bool           `json:"is_default"`
	Config      *string        `json:"config"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type UpdateModel struct {
	ID        *string    `json:"id"`
	Name      *string    `json:"name"`
	Type      *string    `json:"type"`
	Active    *bool      `json:"active"`
	IsDefault *bool      `json:"is_default"`
	Config    *string    `json:"config"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
}
