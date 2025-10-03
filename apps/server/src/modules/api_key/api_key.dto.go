package api_key

import (
	"time"
)

// CreateAPIKeyDto represents the request to create an API key
type CreateAPIKeyDto struct {
	Name          string     `json:"name" validate:"required,min=1,max=255"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	MaxUsageCount *int64     `json:"max_usage_count,omitempty"`
}

// UpdateAPIKeyDto represents the request to update an API key
type UpdateAPIKeyDto struct {
	Name          *string     `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	ExpiresAt     **time.Time `json:"expires_at,omitempty"`
	MaxUsageCount **int64     `json:"max_usage_count,omitempty"`
}

// APIKeyResponse represents the response for API key operations
type APIKeyResponse struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	LastUsed      *time.Time `json:"last_used"`
	ExpiresAt     *time.Time `json:"expires_at"`
	UsageCount    int64      `json:"usage_count"`
	MaxUsageCount *int64     `json:"max_usage_count"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// APIKeyWithTokenResponse represents the response when creating an API key (includes the token)
type APIKeyWithTokenResponse struct {
	APIKeyResponse
	Token string `json:"token"`
}

// ToAPIKeyResponse converts a Model to APIKeyResponse
func (m *Model) ToAPIKeyResponse() *APIKeyResponse {
	return &APIKeyResponse{
		ID:            m.ID,
		Name:          m.Name,
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
