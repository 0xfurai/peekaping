package shared

import (
	"time"
)

type Monitor struct {
	ID string `json:"id"`

	// connection type: http, tcp, ping, etc
	Type string `json:"type" validate:"required" example:"http"`

	// monitor name
	Name string `json:"name" example:"Monitor"`

	// Organization ID
	OrgID string `json:"org_id"`

	// monitor url
	// Url string `json:"url" example:"https://example.com"`

	// monitor interval in seconds to do request to url
	Interval int `json:"interval" example:"60"`

	// monitor timeout in seconds to do request otherwise stop request
	Timeout int `json:"timeout" example:"16"`

	// Maximum retries before the service is marked as down and a notification is sent
	MaxRetries int `json:"max_retries" example:"3"`

	// Retry interval in seconds to do request to url
	RetryInterval int `json:"retry_interval" example:"60"`

	// Resend Notification if Down X times consecutively
	ResendInterval int `json:"resend_interval" example:"10"`

	Active bool          `json:"active"`
	Status MonitorStatus `json:"status"`

	Config    string `json:"config"`
	ProxyId   string `json:"proxy_id"`
	PushToken string `json:"push_token"`

	// Last heartbeat for push monitors
	LastHeartbeat *HeartBeatModel `json:"last_heartbeat,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpdateMonitor struct {
	ID             *string        `json:"id"`
	OrgID          *string        `json:"org_id"`
	Type           *string        `json:"type"`
	Name           *string        `json:"name"`
	Interval       *int           `json:"interval"`
	Timeout        *int           `json:"timeout"`
	MaxRetries     *int           `json:"max_retries"`
	RetryInterval  *int           `json:"retry_interval"`
	ResendInterval *int           `json:"resend_interval"`
	Active         *bool          `json:"active"`
	Status         *MonitorStatus `json:"status"`
	Config         *string        `json:"config"`
	ProxyId        *string        `json:"proxy_id"`
	PushToken      *string        `json:"push_token"`

	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
}
