package migrations

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Gives an install upgraded to workspaces its first one (PP-146, docs/adr/0005): the workspace
// "Default", with every user of the install as its owner and as an instance admin. A self-hosted
// install has one user, so it sees no change.
//
// An install that has no user and no record is a new one, and gets no workspace: the first
// signup makes it. An install that has records but no user (its only user was removed, or the
// records were imported without one) does get the workspace, so that the first signup joins it
// and does not start a second one next to the records.
//
// It is done in Go and not in SQL because the workspace needs an id, and SQLite and PostgreSQL
// don't make them the same way. It changes nothing when a workspace exists already, so a re-run
// leaves what a person did since, such as a user who is no longer an instance admin, alone.

func init() {
	Migrations.MustRegister(adoptExistingInstall, leaveWorkspacesAlone)
}

// adoptionRecordTables are the tables whose rows a workspace will own, apart from the users
var adoptionRecordTables = []string{"monitors", "notification_channels", "proxies", "tags", "maintenances", "status_pages", "api_keys"}

type adoptedWorkspace struct {
	bun.BaseModel `bun:"table:workspaces"`

	ID        string    `bun:"id,pk"`
	Name      string    `bun:"name"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

type adoptedMember struct {
	bun.BaseModel `bun:"table:workspace_members"`

	WorkspaceID string    `bun:"workspace_id"`
	UserID      string    `bun:"user_id"`
	Role        string    `bun:"role"`
	JoinedAt    time.Time `bun:"joined_at"`
}

func adoptExistingInstall(ctx context.Context, db *bun.DB) error {
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		workspaces, err := tx.NewSelect().Table("workspaces").Count(ctx)
		if err != nil || workspaces > 0 {
			return err
		}
		inUse, err := installHasData(ctx, tx)
		if err != nil || !inUse {
			return err
		}

		now := time.Now().UTC()
		workspace := &adoptedWorkspace{ID: uuid.NewString(), Name: "Default", CreatedAt: now, UpdatedAt: now}
		if _, err := tx.NewInsert().Model(workspace).Exec(ctx); err != nil {
			return err
		}

		var userIDs []string
		if err := tx.NewSelect().Table("users").Column("id").Order("created_at", "id").Scan(ctx, &userIDs); err != nil {
			return err
		}
		if len(userIDs) > 0 {
			members := make([]adoptedMember, len(userIDs))
			for i, id := range userIDs {
				members[i] = adoptedMember{WorkspaceID: workspace.ID, UserID: id, Role: "owner", JoinedAt: now}
			}
			// A membership that exists is kept as it is
			if _, err := tx.NewInsert().Model(&members).On("CONFLICT (workspace_id, user_id) DO NOTHING").Exec(ctx); err != nil {
				return err
			}
		}
		_, err = tx.NewUpdate().Table("users").Set("instance_admin = ?", true).Where("TRUE").Exec(ctx)
		return err
	})
}

// installHasData is whether the install has a user or a row in any table a workspace will own
func installHasData(ctx context.Context, tx bun.Tx) (bool, error) {
	for _, table := range append([]string{"users"}, adoptionRecordTables...) {
		found, err := tx.NewSelect().Table(table).Exists(ctx)
		if err != nil || found {
			return found, err
		}
	}
	return false, nil
}

// The tables go with the migration before this one, and the users' flag with them, so there is
// nothing to undo here
func leaveWorkspacesAlone(ctx context.Context, db *bun.DB) error {
	return nil
}
