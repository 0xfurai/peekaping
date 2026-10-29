package status_page

import (
	"context"

	"peekaping/internal/modules/workspace"
)

// Repository stores status pages, which belong to a workspace (docs/adr/0005 section 3). The
// methods that serve a workspace take it right after the context, filter on it, and treat another
// workspace's page as one that doesn't exist: a find returns nil, and a write that changes no row
// is ErrNotFound. The AnyWorkspace methods serve the paths that read across workspaces: public
// pages and badges, and the usage ping.
type Repository interface {
	// Create stores the page in the workspace, whatever workspace the page says
	Create(ctx context.Context, workspaceID workspace.ID, statusPage *Model) (*Model, error)
	FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error)
	// FindByIDAnyWorkspace serves a public page found through its custom domain or a published
	// monitor
	FindByIDAnyWorkspace(ctx context.Context, id string) (*Model, error)
	// FindBySlugAnyWorkspace serves a public page. Slugs are unique across the install.
	FindBySlugAnyWorkspace(ctx context.Context, slug string) (*Model, error)
	FindAll(
		ctx context.Context,
		workspaceID workspace.ID,
		page int,
		limit int,
		q string,
	) ([]*Model, error)
	// FindAllAnyWorkspace lists the pages of every workspace: the usage ping describes the install
	FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error)
	// WorkspaceSuspended is whether the workspace is under a hold that keeps its public pages and
	// badges from answering: the email verification's or an instance admin's (PP-372)
	WorkspaceSuspended(ctx context.Context, workspaceID workspace.ID) (bool, error)
	Update(ctx context.Context, workspaceID workspace.ID, id string, statusPage *UpdateModel) error
	Delete(ctx context.Context, workspaceID workspace.ID, id string) error

	// LockWorkspace holds the workspace until the transaction ctx carries ends, so that a count
	// against one of its limits and the write after it see every other such write before or after
	// them (limits.LockWorkspace)
	LockWorkspace(ctx context.Context, workspaceID workspace.ID) error
	// CountPages is how many status pages the workspace has
	CountPages(ctx context.Context, workspaceID workspace.ID) (int, error)
	// CountDomains is how many custom domains the workspace's pages have, together
	CountDomains(ctx context.Context, workspaceID workspace.ID) (int, error)
}
