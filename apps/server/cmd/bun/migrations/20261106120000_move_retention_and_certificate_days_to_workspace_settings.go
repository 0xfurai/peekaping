package migrations

import (
	"context"
	"database/sql"
	"errors"

	"github.com/uptrace/bun"
)

// Moves the data retention period and the certificate expiry notification days from the install's
// settings to its workspace's settings (PP-150, docs/adr/0005 section 5). An upgraded install has
// one workspace, the oldest, and the values go to it with the same value and type. An install that
// never stored a data retention period keeps the 365 days it used when the setting was unset, and
// one that never stored the notification days keeps 7, 14 and 21.
//
// An install with no workspace is a new one: it has nothing to move, and its first workspace
// starts with the defaults of a new workspace. The two keys are only deleted from its settings.
//
// A workspace that has one of the keys already keeps it, so a re-run changes nothing. The down
// migration copies the oldest workspace's values back to the settings and deletes the keys from
// every workspace.

func init() {
	Migrations.MustRegister(moveWorkspaceSettings, moveWorkspaceSettingsBack)
}

// movedWorkspaceSetting is a setting that belongs to a workspace, and the value an install that
// never stored it has
type movedWorkspaceSetting struct {
	key, unsetValue, valueType string
}

var movedWorkspaceSettings = []movedWorkspaceSetting{
	{key: "KEEP_DATA_PERIOD_DAYS", unsetValue: "365", valueType: "int"},
	{key: "cert_expiry_notify_days", unsetValue: "[7,14,21]", valueType: "json"},
}

func movedKeys() []string {
	keys := make([]string, len(movedWorkspaceSettings))
	for i, s := range movedWorkspaceSettings {
		keys[i] = s.key
	}
	return keys
}

// oldestWorkspaceID returns the ID of the install's oldest workspace, or "" when it has none
func oldestWorkspaceID(ctx context.Context, tx bun.Tx) (string, error) {
	var id string
	err := tx.NewSelect().Table("workspaces").Column("id").Order("created_at", "id").Limit(1).Scan(ctx, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func moveWorkspaceSettings(ctx context.Context, db *bun.DB) error {
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		workspaceID, err := oldestWorkspaceID(ctx, tx)
		if err != nil {
			return err
		}
		if workspaceID != "" {
			for _, moved := range movedWorkspaceSettings {
				value, valueType := moved.unsetValue, moved.valueType
				var stored struct {
					Value string `bun:"value"`
					Type  string `bun:"type"`
				}
				err := tx.NewSelect().Table("settings").Column("value", "type").Where("key = ?", moved.key).Scan(ctx, &stored)
				if err == nil {
					value, valueType = stored.Value, stored.Type
				} else if !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				if _, err := tx.ExecContext(ctx,
					"INSERT INTO workspace_settings (workspace_id, key, value, type) VALUES (?, ?, ?, ?) ON CONFLICT (workspace_id, key) DO NOTHING",
					workspaceID, moved.key, value, valueType); err != nil {
					return err
				}
			}
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM settings WHERE key IN (?)", bun.In(movedKeys()))
		return err
	})
}

func moveWorkspaceSettingsBack(ctx context.Context, db *bun.DB) error {
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		workspaceID, err := oldestWorkspaceID(ctx, tx)
		if err != nil {
			return err
		}
		if workspaceID != "" {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO settings (key, value, type) SELECT key, value, type FROM workspace_settings WHERE workspace_id = ? AND key IN (?) ON CONFLICT (key) DO NOTHING",
				workspaceID, bun.In(movedKeys())); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM workspace_settings WHERE key IN (?)", bun.In(movedKeys()))
		return err
	})
}
