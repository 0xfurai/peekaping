package notification_channel

import "time"

type Model struct {
	ID        string    `json:"id"`
OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Active    bool      `json:"active"`
	IsDefault bool      `json:"is_default"`
	Config    *string   `json:"config"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpdateModel struct {
	ID        *string    `json:"id"`
OrgID     string    `json:"org_id"`
	Name      *string    `json:"name"`
	Type      *string    `json:"type"`
	Active    *bool      `json:"active"`
	IsDefault *bool      `json:"is_default"`
	Config    *string    `json:"config"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
}
