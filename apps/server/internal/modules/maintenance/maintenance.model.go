package maintenance

import (
	"errors"
	"time"

	"peekaping/internal/modules/workspace/workspaceid"
)

// ErrNotFound is a maintenance window that isn't in the workspace of the request: it doesn't exist,
// or it belongs to another workspace, and the caller can't tell which (docs/adr/0005 section 8)
var ErrNotFound = errors.New("maintenance window not found")

type Model struct {
	ID string `json:"id"`
	// WorkspaceID is the workspace the window belongs to. It is stored with the window and read
	// back with it, never taken from a request body, and it is not part of the API's answer.
	WorkspaceID   workspaceid.ID `json:"-"`
	Title         string         `json:"title"`
	Description   string         `json:"description"`
	Active        bool           `json:"active"`
	Strategy      string         `json:"strategy"`
	StartDateTime *string        `json:"start_date_time,omitempty"`
	EndDateTime   *string        `json:"end_date_time,omitempty"`
	StartTime     *string        `json:"start_time,omitempty"`
	EndTime       *string        `json:"end_time,omitempty"`
	Weekdays      []int          `json:"weekdays,omitempty"`
	DaysOfMonth   []int          `json:"days_of_month,omitempty"`
	IntervalDay   *int           `json:"interval_day,omitempty"`
	Cron          *string        `json:"cron,omitempty"`
	Timezone      *string        `json:"timezone,omitempty"`
	Duration      *int           `json:"duration,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type UpdateModel struct {
	Title         *string
	Description   *string
	Active        *bool
	Strategy      *string
	StartDateTime *string
	EndDateTime   *string
	StartTime     *string
	EndTime       *string
	Weekdays      []int
	DaysOfMonth   []int
	IntervalDay   *int
	Cron          *string
	Timezone      *string
	Duration      *int
}
