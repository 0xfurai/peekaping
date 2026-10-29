package workspace

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// The settings that belong to a workspace (docs/adr/0005 section 5). Every other setting belongs
// to the install.
const (
	// DataRetentionPeriodKey holds the data retention period: how many days a workspace keeps its
	// monitors' heartbeats.
	DataRetentionPeriodKey = "KEEP_DATA_PERIOD_DAYS"
	// CertExpiryNotifyDaysKey holds the certificate expiry notification days: a JSON array of the
	// days before a certificate expires at which a workspace's monitors notify.
	CertExpiryNotifyDaysKey = "cert_expiry_notify_days"
)

// DefaultSetting is a setting that a new workspace starts with
type DefaultSetting struct {
	Key   string
	Value string
	Type  string
}

// NewWorkspaceDefaults are the settings every new workspace starts with: the defaults of a new
// install, a data retention period of 180 days and certificate expiry notifications at 7, 14 and
// 21 days.
var NewWorkspaceDefaults = []DefaultSetting{
	{Key: DataRetentionPeriodKey, Value: "180", Type: "int"},
	{Key: CertExpiryNotifyDaysKey, Value: "[7,14,21]", Type: "json"},
}

type settingModel struct {
	bun.BaseModel `bun:"table:workspace_settings,alias:ws"`

	WorkspaceID string    `bun:"workspace_id,pk"`
	Key         string    `bun:"key,pk"`
	Value       string    `bun:"value,notnull"`
	Type        string    `bun:"type,notnull"`
	CreatedAt   time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

// insertDefaultSettings gives a workspace that was just made the settings of a new workspace
func insertDefaultSettings(ctx context.Context, q bun.IDB, id ID) error {
	now := time.Now().UTC()
	rows := make([]settingModel, len(NewWorkspaceDefaults))
	for i, d := range NewWorkspaceDefaults {
		rows[i] = settingModel{WorkspaceID: string(id), Key: d.Key, Value: d.Value, Type: d.Type, CreatedAt: now, UpdatedAt: now}
	}
	_, err := q.NewInsert().Model(&rows).Exec(ctx)
	return err
}
