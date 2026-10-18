package domain_status_page

import (
	"time"
)

type Model struct {
	ID           string    `json:"id"`
	StatusPageID string    `json:"status_page_id"`
	Domain       string    `json:"domain"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type UpdateModel struct {
	StatusPageID *string `json:"status_page_id,omitempty"`
	Domain       *string `json:"domain,omitempty"`
}
