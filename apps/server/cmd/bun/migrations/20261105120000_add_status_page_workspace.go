package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

// Makes a status page belong to a workspace (PP-150, docs/adr/0005 section 2). The status pages of
// an upgraded install go to its one workspace. A page's slug stays unique across the install, and
// so do its custom domains (section 6): their indexes are not touched. The page's domains and
// monitor links belong to its workspace through the page.

func init() {
	Migrations.MustRegister(addStatusPageWorkspace, dropStatusPageWorkspace)
}

func addStatusPageWorkspace(ctx context.Context, db *bun.DB) error {
	return addWorkspaceColumn(ctx, db, "status_pages")
}

func dropStatusPageWorkspace(ctx context.Context, db *bun.DB) error {
	return dropWorkspaceColumn(ctx, db, "status_pages")
}
