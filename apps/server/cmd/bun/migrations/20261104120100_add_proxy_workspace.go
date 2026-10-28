package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

// Makes a proxy belong to a workspace (PP-149, docs/adr/0005 section 2). The proxies of an upgraded install go
// to its one workspace.

func init() {
	Migrations.MustRegister(addProxyWorkspace, dropProxyWorkspace)
}

func addProxyWorkspace(ctx context.Context, db *bun.DB) error {
	return addWorkspaceColumn(ctx, db, "proxies")
}

func dropProxyWorkspace(ctx context.Context, db *bun.DB) error {
	return dropWorkspaceColumn(ctx, db, "proxies")
}
