package monitor

import "vigi/internal/modules/heartbeat"

type CreateUpdateDto struct {
	Type            string   `json:"type" validate:"required" example:"http"`
	Name            string   `json:"name" validate:"required,min=3" example:"My Monitor"`
	Interval        int      `json:"interval" validate:"min=20" example:"60"`
	MaxRetries      int      `json:"max_retries" validate:"min=0" example:"3"`
	RetryInterval   int      `json:"retry_interval" validate:"min=20" example:"60"`
	Timeout         int      `json:"timeout" validate:"min=16" example:"16"`
	ResendInterval  int      `json:"resend_interval" validate:"min=0" example:"10"`
	Active          bool     `json:"active" example:"true"`
	NotificationIds []string `json:"notification_ids" example:"6830ad485361f19c598d6d90"`
	TagIds          []string `json:"tag_ids" example:"6830ad485361f19c598d6d90,6830ad485361f19c598d6d91"`
	ProxyId         string   `json:"proxy_id" example:"6830ad485361f19c598d6d90"`
	Config          string   `json:"config"`
	PushToken       string   `json:"push_token"`
	OrgID           string   `json:"-"` // Set via context/header, not JSON body
}

type PartialUpdateDto struct {
	Name            *string                  `json:"name,omitempty" example:"My Monitor"`
	Interval        *int                     `json:"interval,omitempty" example:"60"`
	Timeout         *int                     `json:"timeout,omitempty" example:"16"`
	Type            *string                  `json:"type,omitempty" example:"http"`
	MaxRetries      *int                     `json:"max_retries,omitempty" example:"3"`
	RetryInterval   *int                     `json:"retry_interval,omitempty" example:"60"`
	ResendInterval  *int                     `json:"resend_interval,omitempty" example:"10"`
	Active          *bool                    `json:"active,omitempty" example:"true"`
	NotificationIds []string                 `json:"notification_ids,omitempty" example:"6830ad485361f19c598d6d90"`
	TagIds          []string                 `json:"tag_ids,omitempty" example:"6830ad485361f19c598d6d90,6830ad485361f19c598d6d91"`
	ProxyId         *string                  `json:"proxy_id,omitempty" example:"6830ad485361f19c598d6d90"`
	Status          *heartbeat.MonitorStatus `json:"status,omitempty" example:"1"`
	Config          *string                  `json:"config,omitempty"`
	PushToken       *string                  `json:"push_token,omitempty"`
	OrgID           *string                  `json:"org_id,omitempty" example:"60c72b2f9b1e8b6f1f8e4b1a"`
}

// UptimeStatsDto represents uptime percentages for various periods
// All values are percentages (0-100)
type UptimeStatsDto struct {
	Uptime24h  float64 `json:"24h"`
	Uptime7d   float64 `json:"7d"`
	Uptime30d  float64 `json:"30d"`
	Uptime365d float64 `json:"365d"`
}

type MonitorResponseDto struct {
	ID              string   `json:"id" example:"60c72b2f9b1e8b6f1f8e4b1a"`
	Name            string   `json:"name" example:"My Monitor"`
	Interval        int      `json:"interval" example:"60"`
	Timeout         int      `json:"timeout" example:"10"`
	Type            string   `json:"type" example:"http"`
	Active          bool     `json:"active" example:"true" default:"true"`
	Status          int      `json:"status" example:"1"`
	MaxRetries      int      `json:"max_retries" example:"3"`
	RetryInterval   int      `json:"retry_interval" example:"10"`
	ResendInterval  int      `json:"resend_interval" example:"3"`
	CreatedAt       string   `json:"created_at" example:"2024-06-01T12:00:00Z"`
	UpdatedAt       string   `json:"updated_at" example:"2024-06-01T12:00:00Z"`
	NotificationIds []string `json:"notification_ids" example:"6830ad485361f19c598d6d90"`
	TagIds          []string `json:"tag_ids" example:"6830ad485361f19c598d6d90,6830ad485361f19c598d6d91"`
	ProxyId         string   `json:"proxy_id" example:"6830ad485361f19c598d6d90"`
	Config          string   `json:"config"`
	PushToken       string   `json:"push_token"`
}

// StatPointsSummaryDto represents stat points and summary for a period
// swagger:model
// @Description Stat points and summary for a period
// @Property points array monitor.StatPoint "Stat points array"
// @Property maxPing number "Maximum ping in the period"
// @Property minPing number "Minimum ping in the period"
// @Property avgPing number "Average ping in the period"
// @Property uptime number "Uptime percentage (0-100) in the period"
type StatPointsSummaryDto struct {
	Points  []*StatPoint `json:"points"`
	MaxPing *float64     `json:"maxPing"`
	MinPing *float64     `json:"minPing"`
	AvgPing *float64     `json:"avgPing"`
	Uptime  *float64     `json:"uptime"`
}

// CustomUptimeStatsDto represents uptime percentages for 24h, 30d, 365d
// All values are percentages (0-100)
type CustomUptimeStatsDto struct {
	Uptime24h  float64 `json:"24h"`
	Uptime7d   float64 `json:"7d"`
	Uptime30d  float64 `json:"30d"`
	Uptime365d float64 `json:"365d"`
}
