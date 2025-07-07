package status_page

import (
	"peekaping/src/modules/shared"
	"time"
)

type CreateStatusPageDTO struct {
	Slug                  string   `json:"slug" validate:"required,min=3"`
	Title                 string   `json:"title" validate:"required,min=3"`
	Description           string   `json:"description"`
	Icon                  string   `json:"icon"`
	Theme                 string   `json:"theme"`
	Published             bool     `json:"published"`
	SearchEngineIndex     bool     `json:"search_engine_index"`
	ShowTags              bool     `json:"show_tags"`
	Password              string   `json:"password,omitempty"`
	FooterText            string   `json:"footer_text"`
	CustomCSS             string   `json:"custom_css"`
	ShowPoweredBy         bool     `json:"show_powered_by"`
	GoogleAnalyticsTagID  string   `json:"google_analytics_tag_id"`
	ShowCertificateExpiry bool     `json:"show_certificate_expiry"`
	AutoRefreshInterval   int      `json:"auto_refresh_interval"`
	MonitorIDs            []string `json:"monitor_ids,omitempty"`
}

type UpdateStatusPageDTO struct {
	Slug                  *string   `json:"slug,omitempty"`
	Title                 *string   `json:"title,omitempty"`
	Description           *string   `json:"description,omitempty"`
	Icon                  *string   `json:"icon,omitempty"`
	Theme                 *string   `json:"theme,omitempty"`
	Published             *bool     `json:"published,omitempty"`
	SearchEngineIndex     *bool     `json:"search_engine_index,omitempty"`
	ShowTags              *bool     `json:"show_tags,omitempty"`
	Password              *string   `json:"password,omitempty"`
	FooterText            *string   `json:"footer_text,omitempty"`
	CustomCSS             *string   `json:"custom_css,omitempty"`
	ShowPoweredBy         *bool     `json:"show_powered_by,omitempty"`
	GoogleAnalyticsTagID  *string   `json:"google_analytics_tag_id,omitempty"`
	ShowCertificateExpiry *bool     `json:"show_certificate_expiry,omitempty"`
	AutoRefreshInterval   *int      `json:"auto_refresh_interval,omitempty"`
	MonitorIDs            *[]string `json:"monitor_ids,omitempty"`
}

type StatusPageWithMonitorsResponseDTO struct {
	ID                    string    `json:"id"`
	Slug                  string    `json:"slug"`
	Title                 string    `json:"title"`
	Description           string    `json:"description"`
	Icon                  string    `json:"icon"`
	Theme                 string    `json:"theme"`
	Published             bool      `json:"published"`
	SearchEngineIndex     bool      `json:"search_engine_index"`
	ShowTags              bool      `json:"show_tags"`
	Password              string    `json:"password,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
	FooterText            string    `json:"footer_text"`
	CustomCSS             string    `json:"custom_css"`
	ShowPoweredBy         bool      `json:"show_powered_by"`
	GoogleAnalyticsTagID  string    `json:"google_analytics_tag_id"`
	ShowCertificateExpiry bool      `json:"show_certificate_expiry"`
	AutoRefreshInterval   int       `json:"auto_refresh_interval"`
	MonitorIDs            []string  `json:"monitor_ids"`
}

type PublicMonitorDTO struct {
	ID     string `json:"id"`
	Type   string `json:"type" validate:"required" example:"http"`
	Name   string `json:"name" example:"Monitor"`
	Active bool   `json:"active"`
}

type PublicHeartbeatDTO struct {
	ID      string               `json:"id"`
	Status  shared.MonitorStatus `json:"status"`
	Time    time.Time            `json:"time"`
	EndTime time.Time            `json:"end_time"`
	Ping    int                  `json:"ping"`
}

type MonitorWithHeartbeatsAndUptimeDTO struct {
	*PublicMonitorDTO
	Heartbeats []*PublicHeartbeatDTO `json:"heartbeats"`
	Uptime24h  float64               `json:"uptime_24h"`
}
