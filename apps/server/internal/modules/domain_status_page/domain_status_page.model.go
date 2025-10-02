package domain_status_page

import (
	"time"
)

type Model struct {
	ID           string    `json:"id" bson:"_id,omitempty"`
	StatusPageID string    `json:"status_page_id" bson:"status_page_id"`
	Domain       string    `json:"domain" bson:"domain"`
	CreatedAt    time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" bson:"updated_at"`
}

type UpdateModel struct {
	StatusPageID *string `json:"status_page_id,omitempty" bson:"status_page_id,omitempty"`
	Domain       *string `json:"domain,omitempty" bson:"domain,omitempty"`
}
