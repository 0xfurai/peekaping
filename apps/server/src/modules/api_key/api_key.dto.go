package api_key

import (
	"time"
)

// CreateAPIKeyDto represents the request to create an API key
// swagger:model
type CreateAPIKeyDto struct {
	Name          string     `json:"name" validate:"required,min=1,max=255"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty" validate:"omitempty"`
	MaxUsageCount *int64     `json:"max_usage_count,omitempty" validate:"omitempty,min=1"`
}

// UpdateAPIKeyDto represents the request to update an API key
// swagger:model
type UpdateAPIKeyDto struct {
	Name          *string     `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	ExpiresAt     *time.Time  `json:"expires_at,omitempty" validate:"omitempty"`
	MaxUsageCount *int64      `json:"max_usage_count,omitempty" validate:"omitempty,min=1"`
}

// APIKeyResponse represents the response for API key operations
// swagger:model
type APIKeyResponse struct {
	ID            string     `json:"id" validate:"required"`
	Name          string     `json:"name" validate:"required"`
	DisplayKey    string     `json:"display_key" validate:"required"` // Masked key for display
	LastUsed      *time.Time `json:"last_used"`
	ExpiresAt     *time.Time `json:"expires_at"`
	UsageCount    int64      `json:"usage_count" validate:"required"`
	MaxUsageCount *int64     `json:"max_usage_count"`
	CreatedAt     time.Time  `json:"created_at" validate:"required"`
	UpdatedAt     time.Time  `json:"updated_at" validate:"required"`
}

// APIKeyConfigResponse represents the API key configuration
// swagger:model
type APIKeyConfigResponse struct {
	Prefix string `json:"prefix" validate:"required"`
}

// APIKeyWithTokenResponse represents the response when creating an API key (includes the token)
// swagger:model
type APIKeyWithTokenResponse struct {
	APIKeyResponse
	Token string `json:"token" validate:"required"`
}

// ToAPIKeyResponse converts a Model to APIKeyResponse
func (m *Model) ToAPIKeyResponse() *APIKeyResponse {
	return &APIKeyResponse{
		ID:            m.ID,
		Name:          m.Name,
		DisplayKey:    m.DisplayKey,
		LastUsed:      m.LastUsed,
		ExpiresAt:     m.ExpiresAt,
		UsageCount:    m.UsageCount,
		MaxUsageCount: m.MaxUsageCount,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
	}
}

// ToAPIKeyWithTokenResponse converts an APIKeyWithToken to APIKeyWithTokenResponse
func (m *APIKeyWithToken) ToAPIKeyWithTokenResponse() *APIKeyWithTokenResponse {
	return &APIKeyWithTokenResponse{
		APIKeyResponse: *m.Model.ToAPIKeyResponse(),
		Token:          m.Token,
	}
}
