package notification_channel

import "time"

type Model struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Active    bool    `json:"active"`
	IsDefault bool    `json:"is_default"`
	Config    *string `json:"config"`
	// Secret holds the channel's credentials. It is never serialized: responses carry
	// config only, and the send path merges the two back together with FullConfig.
	Secret    *string   `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpdateModel struct {
	ID        *string    `json:"id"`
	Name      *string    `json:"name"`
	Type      *string    `json:"type"`
	Active    *bool      `json:"active"`
	IsDefault *bool      `json:"is_default"`
	Config    *string    `json:"config"`
	Secret    *string    `json:"-"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
}
