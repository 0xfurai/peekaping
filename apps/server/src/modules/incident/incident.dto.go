package incident

type CreateIncidentDTO struct {
	Title        string  `json:"title" validate:"required,min=1,max=255"`
	Content      string  `json:"content" validate:"required,min=1"`
	Style        string  `json:"style" validate:"required,oneof=warning info success error"`
	Pin          *bool   `json:"pin"`
	Active       *bool   `json:"active"`
	StatusPageID *string `json:"status_page_id" validate:"omitempty,uuid"`
}

type UpdateIncidentDTO struct {
	Title        *string `json:"title,omitempty" validate:"omitempty,min=1,max=255"`
	Content      *string `json:"content,omitempty" validate:"omitempty,min=1"`
	Style        *string `json:"style,omitempty" validate:"omitempty,oneof=warning info success error"`
	Pin          *bool   `json:"pin,omitempty"`
	Active       *bool   `json:"active,omitempty"`
	StatusPageID *string `json:"status_page_id,omitempty" validate:"omitempty,uuid"`
}
