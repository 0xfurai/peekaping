package monitor_status_page

import (
	"errors"
	"time"
)

// ErrUnknownMonitor is returned, wrapped with the id, when a status page is given a monitor that
// isn't in its workspace: it doesn't exist, or it belongs to another workspace, and the caller
// can't tell which (docs/adr/0005 section 8)
var ErrUnknownMonitor = errors.New("monitor not found")

type Model struct {
	ID           string    `json:"id"`
	StatusPageID string    `json:"status_page_id"`
	MonitorID    string    `json:"monitor_id"`
	Order        int       `json:"order"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type UpdateModel struct {
	StatusPageID *string `json:"status_page_id,omitempty"`
	MonitorID    *string `json:"monitor_id,omitempty"`
	Order        *int    `json:"order,omitempty"`
	Active       *bool   `json:"active,omitempty"`
}
