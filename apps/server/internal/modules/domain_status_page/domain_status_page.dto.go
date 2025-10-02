package domain_status_page

type CreateDto struct {
	StatusPageID string `json:"status_page_id" validate:"required"`
	Domain       string `json:"domain" validate:"required"`
}

type UpdateDto struct {
	StatusPageID *string `json:"status_page_id,omitempty"`
	Domain       *string `json:"domain,omitempty"`
}

type CreateUpdateDto struct {
	StatusPageID string `json:"status_page_id" validate:"required"`
	Domain       string `json:"domain" validate:"required"`
}

type PartialUpdateDto struct {
	StatusPageID *string `json:"status_page_id,omitempty"`
	Domain       *string `json:"domain,omitempty"`
}

// DTOs for managing relationships
type AddDomainToStatusPageDto struct {
	StatusPageID string `json:"status_page_id" validate:"required"`
	Domain       string `json:"domain" validate:"required"`
}

type RemoveDomainFromStatusPageDto struct {
	StatusPageID string `json:"status_page_id" validate:"required"`
	Domain       string `json:"domain" validate:"required"`
}

type GetDomainsForStatusPageDto struct {
	StatusPageID string `json:"status_page_id" validate:"required"`
}

type GetStatusPagesForDomainDto struct {
	Domain string `json:"domain" validate:"required"`
}
