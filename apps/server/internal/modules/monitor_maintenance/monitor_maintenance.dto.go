package monitor_maintenance

type CreateDto struct {
	MonitorID     string `json:"monitor_id"`
	MaintenanceID string `json:"maintenance_id"`
}

type CreateUpdateDto struct {
	MonitorID     string `json:"monitor_id"`
	MaintenanceID string `json:"maintenance_id"`
}
