package status_page

import (
	"peekaping/internal/modules/shared"
	"time"
)

type CreateStatusPageDTO struct {
	Slug                  string   `json:"slug" validate:"required,min=3"`
	Title                 string   `json:"title" validate:"required,min=3"`
	Description           string   `json:"description"`
	Icon                  string   `json:"icon"`
	Theme                 string   `json:"theme"`
	Published             bool     `json:"published"`
	ShowLocations         bool     `json:"show_locations" doc:"List each location of a monitor that runs on several probes on the public page. Off by default."`
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
	Domains               []string `json:"domains,omitempty"`
}

type UpdateStatusPageDTO struct {
	Slug                  *string   `json:"slug,omitempty"`
	Title                 *string   `json:"title,omitempty"`
	Description           *string   `json:"description,omitempty"`
	Icon                  *string   `json:"icon,omitempty"`
	Theme                 *string   `json:"theme,omitempty"`
	Published             *bool     `json:"published,omitempty"`
	ShowLocations         *bool     `json:"show_locations,omitempty" doc:"List each location of a monitor that runs on several probes on the public page."`
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
	Domains               *[]string `json:"domains,omitempty"`
}

type StatusPageWithMonitorsResponseDTO struct {
	ID                    string    `json:"id"`
	Slug                  string    `json:"slug"`
	Title                 string    `json:"title"`
	Description           string    `json:"description"`
	Icon                  string    `json:"icon"`
	Theme                 string    `json:"theme"`
	Published             bool      `json:"published"`
	ShowLocations         bool      `json:"show_locations"`
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
	Domains               []string  `json:"domains"`
}

type PublicMonitorDTO struct {
	ID     string `json:"id"`
	Type   string `json:"type" validate:"required" example:"http"`
	Name   string `json:"name" example:"Monitor"`
	Active bool   `json:"active"`
	// Status is the monitor's own status, which is not the status of its latest heartbeat once the
	// monitor runs on several probes.
	Status shared.MonitorStatus `json:"status" doc:"The monitor's status: 0 Down, 1 Up, 2 Pending, 3 Maintenance" example:"1"`
}

type PublicHeartbeatDTO struct {
	ID      string               `json:"id"`
	Status  shared.MonitorStatus `json:"status"`
	Time    time.Time            `json:"time"`
	EndTime time.Time            `json:"end_time"`
	Ping    int                  `json:"ping"`
}

// PublicLocationDTO is one location of a monitor on a status page that shows them: the probe's name
// and the monitor's results from that probe alone. It names no probe id and says nothing about the
// probe's connection.
type PublicLocationDTO struct {
	Name       string                `json:"name" example:"Frankfurt"`
	Status     shared.MonitorStatus  `json:"status" doc:"The status of this location's latest heartbeat: 0 Down, 1 Up, 2 Pending, 3 Maintenance. Pending until the probe reports." example:"1"`
	Uptime24h  *float64              `json:"uptime_24h" nullable:"true" doc:"The uptime of this location over the last 24 hours, in percent. Null when it could not be read."`
	Heartbeats []*PublicHeartbeatDTO `json:"heartbeats"`
}

type MonitorWithHeartbeatsAndUptimeDTO struct {
	*PublicMonitorDTO
	Heartbeats []*PublicHeartbeatDTO `json:"heartbeats"`
	Uptime24h  float64               `json:"uptime_24h"`
	// Locations are listed only when the status page shows them and the monitor runs on more than
	// one probe.
	Locations []*PublicLocationDTO `json:"locations,omitempty" doc:"One entry for each probe the monitor runs on, local first and then by name. Present only when the status page shows locations and the monitor runs on more than one probe."`
}
