package monitor_tls_info

import (
	"context"
)

type Repository interface {
	// GetByMonitorID retrieves TLS info for a specific monitor
	GetByMonitorID(ctx context.Context, monitorID string) (*Model, error)

	// Create creates a new TLS info record
	Create(ctx context.Context, dto *CreateDto) (*Model, error)

	// Update updates an existing TLS info record
	Update(ctx context.Context, monitorID string, dto *UpdateDto) (*Model, error)

	// Upsert creates or updates TLS info for a monitor
	Upsert(ctx context.Context, monitorID string, infoJSON string) (*Model, error)

	// Delete removes TLS info for a monitor
	Delete(ctx context.Context, monitorID string) error

	// CleanupOldRecords removes TLS info records older than specified days
	CleanupOldRecords(ctx context.Context, olderThanDays int) error
}
