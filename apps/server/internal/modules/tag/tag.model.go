package tag

import (
	"errors"
	"time"

	"peekaping/internal/modules/workspace/workspaceid"
)

// ErrNotFound is a tag that isn't in the workspace of the request: it doesn't exist, or it belongs
// to another workspace, and the caller can't tell which (docs/adr/0005 section 8)
var ErrNotFound = errors.New("tag not found")

// ErrNameTaken is a tag name that another tag of the workspace has. Tag names are unique within a
// workspace and not across the install (docs/adr/0005 section 6).
var ErrNameTaken = errors.New("tag with this name already exists")

type Model struct {
	ID string `json:"id"`
	// WorkspaceID is the workspace the tag belongs to. It is stored with the tag and read back with
	// it, never taken from a request body, and it is not part of the API's answer.
	WorkspaceID workspaceid.ID `json:"-"`
	Name        string         `json:"name"`
	Color       string         `json:"color"`
	Description *string        `json:"description"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type UpdateModel struct {
	ID          *string    `json:"id"`
	Name        *string    `json:"name"`
	Color       *string    `json:"color"`
	Description *string    `json:"description"`
	CreatedAt   *time.Time `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
}
