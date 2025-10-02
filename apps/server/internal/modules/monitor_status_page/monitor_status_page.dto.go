package monitor_status_page

type CreateDto struct {
	StatusPageID string `json:"status_page_id" validate:"required"`
	MonitorID    string `json:"monitor_id" validate:"required"`
	Order        int    `json:"order"`
	Active       bool   `json:"active"`
}

type UpdateDto struct {
	StatusPageID *string `json:"status_page_id,omitempty"`
	MonitorID    *string `json:"monitor_id,omitempty"`
	Order        *int    `json:"order,omitempty"`
	Active       *bool   `json:"active,omitempty"`
}

type CreateUpdateDto struct {
	StatusPageID string `json:"status_page_id" validate:"required"`
	MonitorID    string `json:"monitor_id" validate:"required"`
	Order        int    `json:"order"`
	Active       bool   `json:"active"`
}

type PartialUpdateDto struct {
	StatusPageID *string `json:"status_page_id,omitempty"`
	MonitorID    *string `json:"monitor_id,omitempty"`
	Order        *int    `json:"order,omitempty"`
	Active       *bool   `json:"active,omitempty"`
}

// DTOs for managing relationships
type AddMonitorToStatusPageDto struct {
	StatusPageID string `json:"status_page_id" validate:"required"`
	MonitorID    string `json:"monitor_id" validate:"required"`
	Order        int    `json:"order"`
	Active       bool   `json:"active"`
}

type RemoveMonitorFromStatusPageDto struct {
	StatusPageID string `json:"status_page_id" validate:"required"`
	MonitorID    string `json:"monitor_id" validate:"required"`
}

type GetMonitorsForStatusPageDto struct {
	StatusPageID string `json:"status_page_id" validate:"required"`
}

type GetStatusPagesForMonitorDto struct {
	MonitorID string `json:"monitor_id" validate:"required"`
}
