package incident

import "time"

type Model struct {
	ID           string    `json:"id" bson:"_id,omitempty"`
	Title        string    `json:"title" bson:"title"`
	Content      string    `json:"content" bson:"content"`
	Style        string    `json:"style" bson:"style"`
	Pin          bool      `json:"pin" bson:"pin"`
	Active       bool      `json:"active" bson:"active"`
	StatusPageID *string   `json:"status_page_id" bson:"status_page_id"`
	CreatedAt    time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" bson:"updated_at"`
}

type UpdateModel struct {
	Title        *string `json:"title,omitempty" bson:"title,omitempty"`
	Content      *string `json:"content,omitempty" bson:"content,omitempty"`
	Style        *string `json:"style,omitempty" bson:"style,omitempty"`
	Pin          *bool   `json:"pin,omitempty" bson:"pin,omitempty"`
	Active       *bool   `json:"active,omitempty" bson:"active,omitempty"`
	StatusPageID *string `json:"status_page_id,omitempty" bson:"status_page_id,omitempty"`
}
