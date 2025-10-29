package monitor_maintenance

import "time"

type Model struct {
	ID            string    `json:"id"`
	MonitorID     string    `json:"monitor_id"`
	MaintenanceID string    `json:"maintenance_id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
