package monitor_tls_info

import (
	"peekaping/internal/modules/shared"
	"time"
)

type Model struct {
	ID        string    `json:"id"`
	MonitorID string    `json:"monitor_id"`
	InfoJSON  string    `json:"info_json"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Type aliases for shared types
type CertificateInfo = shared.CertificateInfo
type TLSInfo = shared.TLSInfo
