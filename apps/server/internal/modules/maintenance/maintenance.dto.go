package maintenance

import (
	"encoding/json"
	"time"

	"peekaping/internal/utils"
)

type CreateUpdateDto struct {
	Title         string   `json:"title" validate:"required"`
	Description   string   `json:"description"`
	Active        bool     `json:"active"`
	Strategy      string   `json:"strategy" validate:"required"`
	StartDateTime *string  `json:"start_date_time,omitempty" validate:"omitempty,datetime=2006-01-02T15:04"`
	EndDateTime   *string  `json:"end_date_time,omitempty" validate:"omitempty,datetime=2006-01-02T15:04"`
	StartTime     *string  `json:"start_time,omitempty" validate:"omitempty,datetime=15:04"`
	EndTime       *string  `json:"end_time,omitempty" validate:"omitempty,datetime=15:04"`
	Weekdays      []int    `json:"weekdays,omitempty" validate:"dive,min=0,max=6"`
	DaysOfMonth   []int    `json:"days_of_month,omitempty"`
	IntervalDay   *int     `json:"interval_day,omitempty"`
	Cron          *string  `json:"cron,omitempty"`
	Timezone      *string  `json:"timezone,omitempty"`
	Duration      *int     `json:"duration,omitempty" validate:"omitempty,min=1"`
	MonitorIds    []string `json:"monitor_ids,omitempty"`
	activeSet     bool
}

// UnmarshalJSON records whether active was present so create requests can apply
// the historical default without changing full-update semantics.
func (dto *CreateUpdateDto) UnmarshalJSON(data []byte) error {
	type plain CreateUpdateDto
	*dto = CreateUpdateDto{}
	if err := json.Unmarshal(data, (*plain)(dto)); err != nil {
		return err
	}
	var err error
	dto.activeSet, err = utils.HasJSONKey(data, "active")
	return err
}

// ApplyCreateDefaults keeps omitted active fields active for create requests.
// Full updates keep their existing zero-value semantics.
func (dto *CreateUpdateDto) ApplyCreateDefaults() {
	if dto == nil {
		return
	}
	if !dto.activeSet {
		dto.Active = true
	}
}

type PartialUpdateDto struct {
	Title         *string  `json:"title,omitempty"`
	Description   *string  `json:"description,omitempty"`
	Active        *bool    `json:"active,omitempty"`
	Strategy      *string  `json:"strategy,omitempty"`
	StartDateTime *string  `json:"start_date_time,omitempty" validate:"omitempty,datetime=2006-01-02T15:04"`
	EndDateTime   *string  `json:"end_date_time,omitempty" validate:"omitempty,datetime=2006-01-02T15:04"`
	StartTime     *string  `json:"start_time,omitempty" validate:"omitempty,datetime=15:04"`
	EndTime       *string  `json:"end_time,omitempty" validate:"omitempty,datetime=15:04"`
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
	StartTime     *string   `json:"start_time,omitempty" validate:"omitempty,datetime=15:04"`
	EndTime       *string   `json:"end_time,omitempty" validate:"omitempty,datetime=15:04"`
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

func (dto *CreateUpdateDto) toModel() *Model {
	return &Model{
		Title:         dto.Title,
		Description:   dto.Description,
		Active:        dto.Active,
		Strategy:      dto.Strategy,
		StartDateTime: dto.StartDateTime,
		EndDateTime:   dto.EndDateTime,
		StartTime:     dto.StartTime,
		EndTime:       dto.EndTime,
		Weekdays:      dto.Weekdays,
		DaysOfMonth:   dto.DaysOfMonth,
		IntervalDay:   dto.IntervalDay,
		Cron:          dto.Cron,
		Timezone:      dto.Timezone,
		Duration:      dto.Duration,
	}
}

func (dto *PartialUpdateDto) toModel() *UpdateModel {
	return &UpdateModel{
		Title:         dto.Title,
		Description:   dto.Description,
		Active:        dto.Active,
		Strategy:      dto.Strategy,
		StartDateTime: dto.StartDateTime,
		EndDateTime:   dto.EndDateTime,
		StartTime:     dto.StartTime,
		EndTime:       dto.EndTime,
		Weekdays:      dto.Weekdays,
		DaysOfMonth:   dto.DaysOfMonth,
		IntervalDay:   dto.IntervalDay,
		Cron:          dto.Cron,
		Timezone:      dto.Timezone,
		Duration:      dto.Duration,
	}
}

// dtoOf is the create route's body for a window, to hold it to the rules of the DTO
func dtoOf(m *Model) *CreateUpdateDto {
	return &CreateUpdateDto{
		Title: m.Title, Description: m.Description, Active: m.Active, Strategy: m.Strategy,
		StartDateTime: m.StartDateTime, EndDateTime: m.EndDateTime, StartTime: m.StartTime, EndTime: m.EndTime,
		Weekdays: m.Weekdays, DaysOfMonth: m.DaysOfMonth, IntervalDay: m.IntervalDay,
		Cron: m.Cron, Timezone: m.Timezone, Duration: m.Duration,
	}
}
