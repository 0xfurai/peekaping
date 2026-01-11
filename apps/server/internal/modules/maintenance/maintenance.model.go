package maintenance

import "time"

type Model struct {
	ID            string    `json:"id"`
	OrgID         string    `json:"org_id"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	Active        bool      `json:"active"`
	Strategy      string    `json:"strategy"`
	StartDateTime *string   `json:"start_date_time,omitempty"`
	EndDateTime   *string   `json:"end_date_time,omitempty"`
	StartTime     *string   `json:"start_time,omitempty"`
	EndTime       *string   `json:"end_time,omitempty"`
	Weekdays      []int     `json:"weekdays,omitempty"`
	DaysOfMonth   []int     `json:"days_of_month,omitempty"`
	IntervalDay   *int      `json:"interval_day,omitempty"`
	Cron          *string   `json:"cron,omitempty"`
	Timezone      *string   `json:"timezone,omitempty"`
	Duration      *int      `json:"duration,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
