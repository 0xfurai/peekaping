package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

// Makes an API key belong to a workspace (PP-147, docs/adr/0005 section 2). The keys of an upgraded
// install go to its one workspace, and keep working.

func init() {
	Migrations.MustRegister(addAPIKeyWorkspace, dropAPIKeyWorkspace)
}

func addAPIKeyWorkspace(ctx context.Context, db *bun.DB) error {
	return addWorkspaceColumn(ctx, db, "api_keys")
}

func dropAPIKeyWorkspace(ctx context.Context, db *bun.DB) error {
	return dropWorkspaceColumn(ctx, db, "api_keys")
}
