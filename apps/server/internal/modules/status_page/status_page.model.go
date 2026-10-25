package status_page

import "time"

type Model struct {
	ID                  string `json:"id"`
	Slug                string `json:"slug"`
	Title               string `json:"title"`
	Description         string `json:"description"`
	Icon                string `json:"icon"`
	Theme               string `json:"theme"`
	Published           bool   `json:"published"`
	ShowLocations       bool   `json:"show_locations"`
	FooterText          string `json:"footer_text"`
	AutoRefreshInterval int    `json:"auto_refresh_interval"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpdateModel struct {
	Slug                *string `json:"slug,omitempty"`
	Title               *string `json:"title,omitempty"`
	Description         *string `json:"description,omitempty"`
	Icon                *string `json:"icon,omitempty"`
	Theme               *string `json:"theme,omitempty"`
	Published           *bool   `json:"published,omitempty"`
	ShowLocations       *bool   `json:"show_locations,omitempty"`
	FooterText          *string `json:"footer_text,omitempty"`
	AutoRefreshInterval *int    `json:"auto_refresh_interval,omitempty"`
}
