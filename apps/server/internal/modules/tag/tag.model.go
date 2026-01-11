package tag

import "time"

type Model struct {
	ID          string    `json:"id"`
OrgID     string    `json:"org_id"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	Description *string   `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type UpdateModel struct {
	ID          *string    `json:"id"`
OrgID     string    `json:"org_id"`
	Name        *string    `json:"name"`
	Color       *string    `json:"color"`
	Description *string    `json:"description"`
	CreatedAt   *time.Time `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
}
