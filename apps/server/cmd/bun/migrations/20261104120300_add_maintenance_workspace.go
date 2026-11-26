package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

// Makes a maintenance window belong to a workspace (PP-149, docs/adr/0005 section 2). The maintenance windows of an upgraded install go
// to its one workspace.

func init() {
	Migrations.MustRegister(addMaintenanceWorkspace, dropMaintenanceWorkspace)
}

func addMaintenanceWorkspace(ctx context.Context, db *bun.DB) error {
	return addWorkspaceColumn(ctx, db, "maintenances")
}

func dropMaintenanceWorkspace(ctx context.Context, db *bun.DB) error {
	return dropWorkspaceColumn(ctx, db, "maintenances")
}
