package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

// Makes a monitor belong to a workspace (PP-148, docs/adr/0005 section 2). The monitors of an
// upgraded install go to its one workspace.

func init() {
	Migrations.MustRegister(addMonitorWorkspace, dropMonitorWorkspace)
}

func addMonitorWorkspace(ctx context.Context, db *bun.DB) error {
	return addWorkspaceColumn(ctx, db, "monitors")
}

func dropMonitorWorkspace(ctx context.Context, db *bun.DB) error {
	return dropWorkspaceColumn(ctx, db, "monitors")
}
