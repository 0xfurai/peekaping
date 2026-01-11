package status_page

import "time"

type Model struct {
	ID                  string `json:"id" bson:"_id,omitempty"`
OrgID     string    `json:"org_id"`
	Slug                string `json:"slug" bson:"slug"`
	Title               string `json:"title" bson:"title"`
	Description         string `json:"description" bson:"description"`
	Icon                string `json:"icon" bson:"icon"`
	Theme               string `json:"theme" bson:"theme"`
	Published           bool   `json:"published" bson:"published"`
	FooterText          string `json:"footer_text" bson:"footer_text"`
	AutoRefreshInterval int    `json:"auto_refresh_interval" bson:"auto_refresh_interval"`

	CreatedAt time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt time.Time `json:"updated_at" bson:"updated_at"`
}

type UpdateModel struct {
	Slug                *string `json:"slug,omitempty" bson:"slug,omitempty"`
	Title               *string `json:"title,omitempty" bson:"title,omitempty"`
	Description         *string `json:"description,omitempty" bson:"description,omitempty"`
	Icon                *string `json:"icon,omitempty" bson:"icon,omitempty"`
	Theme               *string `json:"theme,omitempty" bson:"theme,omitempty"`
	Published           *bool   `json:"published,omitempty" bson:"published,omitempty"`
	FooterText          *string `json:"footer_text,omitempty" bson:"footer_text,omitempty"`
	AutoRefreshInterval *int    `json:"auto_refresh_interval,omitempty" bson:"auto_refresh_interval,omitempty"`
}
