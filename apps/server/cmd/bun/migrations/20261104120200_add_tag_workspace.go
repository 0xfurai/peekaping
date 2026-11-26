package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

// Makes a tag belong to a workspace (PP-149, docs/adr/0005 section 2). The tags of an upgraded install go
// to its one workspace.

func init() {
	Migrations.MustRegister(addTagWorkspace, dropTagWorkspace)
}

func addTagWorkspace(ctx context.Context, db *bun.DB) error {
	return addWorkspaceColumn(ctx, db, "tags")
}

func dropTagWorkspace(ctx context.Context, db *bun.DB) error {
	return dropWorkspaceColumn(ctx, db, "tags")
}
