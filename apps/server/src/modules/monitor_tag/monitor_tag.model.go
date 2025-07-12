package monitor_tag

import "time"

type Model struct {
	ID        string    `json:"id"`
	MonitorID string    `json:"monitor_id"`
	TagID     string    `json:"tag_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
