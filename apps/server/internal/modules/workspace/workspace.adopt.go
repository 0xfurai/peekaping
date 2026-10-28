package workspace

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// FirstWorkspaceName is the name of the workspace that an install gets when it first has one.
const FirstWorkspaceName = "My workspace"

// recordTables are the tables whose rows a workspace will own, apart from the users.
var recordTables = []string{"monitors", "notification_channels", "proxies", "tags", "maintenances", "status_pages", "api_keys"}

// Adoption says what AdoptExistingInstall made.
type Adoption struct {
	Workspace Workspace
	// Owners is how many users it made owners and instance admins of the workspace.
	Owners int
}

// AdoptExistingInstall gives an install that has data and no workspace its first one, as the
// upgrade to workspaces does (the migration adopt_existing_install): the workspace "My workspace",
// with every user as its owner and an instance admin. It returns nil, and changes nothing, when
// the install already has a workspace, and when it has no user and no record, which is a new
// install that the first signup gives its workspace.
//
// It is for the code that fills a database with an install's data, such as the importer of a
// MongoDB install, which runs after the upgrade has migrated an empty database. It writes through
// q, so the caller says in which transaction.
func AdoptExistingInstall(ctx context.Context, q bun.IDB) (*Adoption, error) {
	existing, err := q.NewSelect().Table("workspaces").Exists(ctx)
	if err != nil || existing {
		return nil, err
	}
	inUse, err := hasData(ctx, q)
	if err != nil || !inUse {
		return nil, err
	}

	now := time.Now().UTC()
	sm := &sqlModel{ID: uuid.NewString(), Name: FirstWorkspaceName, CreatedAt: now, UpdatedAt: now}
	if _, err := q.NewInsert().Model(sm).Exec(ctx); err != nil {
		return nil, err
	}

	var userIDs []string
	if err := q.NewSelect().Table("users").Column("id").Order("created_at", "id").Scan(ctx, &userIDs); err != nil {
		return nil, err
	}
	if len(userIDs) > 0 {
		members := make([]memberModel, len(userIDs))
		for i, id := range userIDs {
			members[i] = memberModel{WorkspaceID: sm.ID, UserID: id, Role: string(RoleOwner), JoinedAt: now}
		}
		if _, err := q.NewInsert().Model(&members).On("CONFLICT (workspace_id, user_id) DO NOTHING").Exec(ctx); err != nil {
			return nil, err
		}
		if _, err := q.NewUpdate().Table("users").Set("instance_admin = ?", true).Where("TRUE").Exec(ctx); err != nil {
			return nil, err
		}
	}
	return &Adoption{Workspace: *sm.toDomain(), Owners: len(userIDs)}, nil
}

func hasData(ctx context.Context, q bun.IDB) (bool, error) {
	for _, table := range append([]string{"users"}, recordTables...) {
		found, err := q.NewSelect().Table(table).Exists(ctx)
		if err != nil || found {
			return found, err
		}
	}
	return false, nil
}
