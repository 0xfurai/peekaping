package monitor_maintenance

import (
	"errors"
	"time"
)

// ErrUnknownMonitor is returned, wrapped with the id, when a maintenance window is given a monitor
// that isn't in its workspace: it doesn't exist, or it belongs to another workspace, and the caller
// can't tell which (docs/adr/0005 section 8)
var ErrUnknownMonitor = errors.New("monitor not found")

type Model struct {
	ID            string    `json:"id"`
	MonitorID     string    `json:"monitor_id"`
	MaintenanceID string    `json:"maintenance_id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
