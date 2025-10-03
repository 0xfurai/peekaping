package api_key

import (
	"time"
)

// Model represents an API key in the domain
type Model struct {
	ID             string     `json:"id"`
	UserID         string     `json:"user_id"`
	Name           string     `json:"name"`
	KeyHash        string     `json:"-"` // Never expose the hash
	DisplayKey     string     `json:"display_key"` // Masked key for display (e.g., "pk_1234...5678")
	LastUsed       *time.Time `json:"last_used"`
	ExpiresAt      *time.Time `json:"expires_at"`
	UsageCount     int64      `json:"usage_count"`
	MaxUsageCount  *int64     `json:"max_usage_count"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// CreateModel represents data needed to create an API key
type CreateModel struct {
	UserID         string     `json:"user_id"`
	Name           string     `json:"name"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	MaxUsageCount  *int64     `json:"max_usage_count,omitempty"`
}

// UpdateModel represents data that can be updated for an API key
type UpdateModel struct {
	Name          *string    `json:"name,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	MaxUsageCount *int64     `json:"max_usage_count,omitempty"`
}


// APIKeyWithToken represents an API key with its plain text token (only returned on creation)
type APIKeyWithToken struct {
	Model
	Token string `json:"token"` // Only present when creating a new key
}

