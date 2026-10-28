package status_page

import (
	"errors"
	"time"

	"peekaping/internal/modules/workspace/workspaceid"
)

// ErrNotFound is a status page that isn't in the workspace of the request: it doesn't exist, or it
// belongs to another workspace, and the caller can't tell which (docs/adr/0005 section 8)
var ErrNotFound = errors.New("status page not found")

type Model struct {
	ID string `json:"id"`
	// WorkspaceID is the workspace the page belongs to. It is stored with the page and read back
	// with it, never taken from a request body, and it is not part of the API's answer.
	WorkspaceID         workspaceid.ID `json:"-"`
	Slug                string         `json:"slug"`
	Title               string         `json:"title"`
	Description         string         `json:"description"`
	Icon                string         `json:"icon"`
	Theme               string         `json:"theme"`
	Published           bool           `json:"published"`
	ShowLocations       bool           `json:"show_locations"`
	FooterText          string         `json:"footer_text"`
	AutoRefreshInterval int            `json:"auto_refresh_interval"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpdateModel struct {
	Slug                *string `json:"slug,omitempty"`
	Title               *string `json:"title,omitempty"`
	Description         *string `json:"description,omitempty"`
	Icon                *string `json:"icon,omitempty"`
	Theme               *string `json:"theme,omitempty"`
	Published           *bool   `json:"published,omitempty"`
	ShowLocations       *bool   `json:"show_locations,omitempty"`
	FooterText          *string `json:"footer_text,omitempty"`
	AutoRefreshInterval *int    `json:"auto_refresh_interval,omitempty"`
}
