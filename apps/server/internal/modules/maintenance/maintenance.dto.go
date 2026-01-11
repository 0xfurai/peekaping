package maintenance

import "time"

type CreateUpdateDto struct {
	OrgID         string   `json:"org_id,omitempty"`
	Title         string   `json:"title" validate:"required"`
	Description   string   `json:"description"`
	Active        bool     `json:"active"`
	Strategy      string   `json:"strategy" validate:"required"`
	StartDateTime *string  `json:"start_date_time,omitempty" validate:"omitempty,datetime=2006-01-02T15:04"`
	EndDateTime   *string  `json:"end_date_time,omitempty" validate:"omitempty,datetime=2006-01-02T15:04"`
	StartTime     *string  `json:"start_time,omitempty" validate:"regexp=^(?:[01]\d|2[0-3]):[0-5]\d$"`
	EndTime       *string  `json:"end_time,omitempty" validate:"regexp=^(?:[01]\d|2[0-3]):[0-5]\d$"`
	Weekdays      []int    `json:"weekdays,omitempty" validate:"dive,min=0,max=6"`
	DaysOfMonth   []int    `json:"days_of_month,omitempty"`
	IntervalDay   *int     `json:"interval_day,omitempty"`
	Cron          *string  `json:"cron,omitempty"`
	Timezone      *string  `json:"timezone,omitempty"`
	Duration      *int     `json:"duration,omitempty" validate:"omitempty,min=1"`
	MonitorIds    []string `json:"monitor_ids,omitempty"`
}

type PartialUpdateDto struct {
	OrgID         *string  `json:"org_id,omitempty"`
	Title         *string  `json:"title,omitempty"`
	Description   *string  `json:"description,omitempty"`
	Active        *bool    `json:"active,omitempty"`
	Strategy      *string  `json:"strategy,omitempty"`
	StartDateTime *string  `json:"start_date_time,omitempty" validate:"omitempty,datetime=2006-01-02T15:04"`
	EndDateTime   *string  `json:"end_date_time,omitempty" validate:"omitempty,datetime=2006-01-02T15:04"`
	StartTime     *string  `json:"start_time,omitempty" validate:"regexp=^(?:[01]\d|2[0-3]):[0-5]\d$"`
	EndTime       *string  `json:"end_time,omitempty" validate:"regexp=^(?:[01]\d|2[0-3]):[0-5]\d$"`
	Weekdays      []int    `json:"weekdays,omitempty" validate:"dive,min=0,max=6"`
	DaysOfMonth   []int    `json:"days_of_month,omitempty"`
	IntervalDay   *int     `json:"interval_day,omitempty"`
	Cron          *string  `json:"cron,omitempty"`
	Timezone      *string  `json:"timezone,omitempty"`
	Duration      *int     `json:"duration,omitempty" validate:"omitempty,min=1"`
	MonitorIds    []string `json:"monitor_ids,omitempty"`
}

type MaintenanceResponseDto struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	Active        bool      `json:"active"`
	Strategy      string    `json:"strategy"`
	StartDateTime *string   `json:"start_date_time,omitempty" validate:"omitempty,datetime=2006-01-02T15:04"`
	EndDateTime   *string   `json:"end_date_time,omitempty" validate:"omitempty,datetime=2006-01-02T15:04"`
	StartTime     *string   `json:"start_time,omitempty" validate:"regexp=^(?:[01]\d|2[0-3]):[0-5]\d$"`
	EndTime       *string   `json:"end_time,omitempty" validate:"regexp=^(?:[01]\d|2[0-3]):[0-5]\d$"`
	Weekdays      []int     `json:"weekdays,omitempty"`
	DaysOfMonth   []int     `json:"days_of_month,omitempty"`
	IntervalDay   *int      `json:"interval_day,omitempty"`
	Cron          *string   `json:"cron,omitempty"`
	Timezone      *string   `json:"timezone,omitempty"`
	Duration      *int      `json:"duration,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	MonitorIds    []string  `json:"monitor_ids"`
}
