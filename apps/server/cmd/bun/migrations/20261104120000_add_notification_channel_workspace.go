package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

// Makes a notification channel belong to a workspace (PP-149, docs/adr/0005 section 2). The channels of an upgraded install go
// to its one workspace.

func init() {
	Migrations.MustRegister(addNotificationChannelWorkspace, dropNotificationChannelWorkspace)
}

func addNotificationChannelWorkspace(ctx context.Context, db *bun.DB) error {
	return addWorkspaceColumn(ctx, db, "notification_channels")
}

func dropNotificationChannelWorkspace(ctx context.Context, db *bun.DB) error {
	return dropWorkspaceColumn(ctx, db, "notification_channels")
}
