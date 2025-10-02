package monitor_status_page

import (
	"time"
)

type Model struct {
	ID           string    `json:"id" bson:"_id,omitempty"`
	StatusPageID string    `json:"status_page_id" bson:"status_page_id"`
	MonitorID    string    `json:"monitor_id" bson:"monitor_id"`
	Order        int       `json:"order" bson:"order"`
	Active       bool      `json:"active" bson:"active"`
	CreatedAt    time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" bson:"updated_at"`
}

type UpdateModel struct {
	StatusPageID *string `json:"status_page_id,omitempty" bson:"status_page_id,omitempty"`
	MonitorID    *string `json:"monitor_id,omitempty" bson:"monitor_id,omitempty"`
	Order        *int    `json:"order,omitempty" bson:"order,omitempty"`
	Active       *bool   `json:"active,omitempty" bson:"active,omitempty"`
}
