package workspace

import (
	"time"
)

type Model struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type UpdateModel struct {
	Name *string `json:"name"`
}
