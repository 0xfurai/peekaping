package monitor_status_page

import (
	"time"
)

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
