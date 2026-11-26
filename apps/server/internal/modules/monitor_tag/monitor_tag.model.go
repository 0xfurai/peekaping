package monitor_tag

import (
	"errors"
	"time"
)

// ErrUnknownTag is returned, wrapped with the id, when a monitor is given a tag that isn't in its
// workspace: it doesn't exist, or it belongs to another workspace, and the caller can't tell which
// (docs/adr/0005 section 8)
var ErrUnknownTag = errors.New("tag not found")

type Model struct {
	ID        string    `json:"id"`
	MonitorID string    `json:"monitor_id"`
	TagID     string    `json:"tag_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
