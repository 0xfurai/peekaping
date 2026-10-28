package workspace

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"peekaping/internal/dbtx"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type sqlModel struct {
	bun.BaseModel `bun:"table:workspaces,alias:w"`

	ID        string    `bun:"id,pk"`
	Name      string    `bun:"name,notnull"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	// SuspendedAt and SuspendedReason are NULL for a workspace that runs
	SuspendedAt     *time.Time `bun:"suspended_at"`
	SuspendedReason *string    `bun:"suspended_reason"`
}

func (m *sqlModel) toDomain() *Workspace {
	return &Workspace{
		ID: ID(m.ID), Name: m.Name, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		SuspendedAt: utcOrNil(m.SuspendedAt), SuspendedReason: reasonOrEmpty(m.SuspendedReason),
	}
}

func utcOrNil(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

func reasonOrEmpty(reason *string) string {
	if reason == nil {
		return ""
	}
	return *reason
}

type memberModel struct {
	bun.BaseModel `bun:"table:workspace_members,alias:m"`

	WorkspaceID string    `bun:"workspace_id,pk"`
	UserID      string    `bun:"user_id,pk"`
	Role        string    `bun:"role,notnull"`
	JoinedAt    time.Time `bun:"joined_at,nullzero,notnull,default:current_timestamp"`
}

// membershipRow is a member joined with its workspace
type membershipRow struct {
	WorkspaceID string    `bun:"workspace_id"`
	Name        string    `bun:"name"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
	Role        string    `bun:"role"`
	JoinedAt    time.Time `bun:"joined_at"`

	SuspendedAt     *time.Time `bun:"suspended_at"`
	SuspendedReason *string    `bun:"suspended_reason"`
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) Create(ctx context.Context, name string) (*Workspace, error) {
	return r.create(ctx, name, "")
}

func (r *SQLRepositoryImpl) CreateSuspended(ctx context.Context, name string, reason string) (*Workspace, error) {
	return r.create(ctx, name, reason)
}

// create adds the workspace, suspended for the reason when there is one
func (r *SQLRepositoryImpl) create(ctx context.Context, name, reason string) (*Workspace, error) {
	now := time.Now().UTC()
	sm := &sqlModel{ID: uuid.NewString(), Name: name, CreatedAt: now, UpdatedAt: now}
	if reason != "" {
		sm.SuspendedAt, sm.SuspendedReason = &now, &reason
	}
	// The workspace and its settings are made together, in the transaction the context carries or
	// one of their own
	err := dbtx.NewSQLTransactor(r.db).WithTx(ctx, func(ctx context.Context) error {
		q := dbtx.DB(ctx, r.db)
		if _, err := q.NewInsert().Model(sm).Exec(ctx); err != nil {
			return err
		}
		return insertDefaultSettings(ctx, q, ID(sm.ID))
	})
	if err != nil {
		return nil, err
	}
	return sm.toDomain(), nil
}

func (r *SQLRepositoryImpl) AddMember(ctx context.Context, id ID, userID string, role Role) error {
	mm := &memberModel{WorkspaceID: string(id), UserID: userID, Role: string(role), JoinedAt: time.Now().UTC()}
	_, err := dbtx.DB(ctx, r.db).NewInsert().Model(mm).Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) ListMemberships(ctx context.Context, userID string) ([]Membership, error) {
	var rows []membershipRow
	err := dbtx.DB(ctx, r.db).NewSelect().
		TableExpr("workspace_members AS m").
		Join("JOIN workspaces AS w ON w.id = m.workspace_id").
		ColumnExpr("w.id AS workspace_id, w.name, w.created_at, w.updated_at, w.suspended_at, w.suspended_reason, m.role, m.joined_at").
		Where("m.user_id = ?", userID).
		Order("m.joined_at", "w.id").
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	memberships := make([]Membership, len(rows))
	for i, row := range rows {
		memberships[i] = Membership{
			Workspace: Workspace{
				ID: ID(row.WorkspaceID), Name: row.Name, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
				SuspendedAt: utcOrNil(row.SuspendedAt), SuspendedReason: reasonOrEmpty(row.SuspendedReason),
			},
			Role:     Role(row.Role),
			JoinedAt: row.JoinedAt,
		}
	}
	return memberships, nil
}

func (r *SQLRepositoryImpl) FindByID(ctx context.Context, id ID) (*Workspace, error) {
	sm := new(sqlModel)
	err := dbtx.DB(ctx, r.db).NewSelect().Model(sm).Where("w.id = ?", string(id)).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sm.toDomain(), nil
}

func (r *SQLRepositoryImpl) FindOldest(ctx context.Context) (*Workspace, error) {
	sm := new(sqlModel)
	err := dbtx.DB(ctx, r.db).NewSelect().Model(sm).Order("w.created_at", "w.id").Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sm.toDomain(), nil
}

func (r *SQLRepositoryImpl) ListIDsAnyWorkspace(ctx context.Context) ([]ID, error) {
	var ids []string
	if err := dbtx.DB(ctx, r.db).NewSelect().Table("workspaces").Column("id").Order("created_at", "id").Scan(ctx, &ids); err != nil {
		return nil, err
	}
	out := make([]ID, len(ids))
	for i, id := range ids {
		out[i] = ID(id)
	}
	return out, nil
}

func (r *SQLRepositoryImpl) LiftUnverifiedOwnedBy(ctx context.Context, userID string) ([]ID, error) {
	var lifted []ID
	// Read and write in one transaction, so that the IDs returned are the ones that were lifted
	err := dbtx.NewSQLTransactor(r.db).WithTx(ctx, func(ctx context.Context) error {
		q := dbtx.DB(ctx, r.db)
		var ids []string
		err := q.NewSelect().Table("workspaces").Column("id").
			Where("suspended_reason = ?", SuspendedUnverified).
			Where("id IN (SELECT workspace_id FROM workspace_members WHERE user_id = ? AND role = ?)", userID, string(RoleOwner)).
			Order("created_at", "id").
			Scan(ctx, &ids)
		if err != nil || len(ids) == 0 {
			return err
		}
		_, err = q.NewUpdate().Table("workspaces").
			Set("suspended_at = NULL").
			Set("suspended_reason = NULL").
			Set("updated_at = ?", time.Now().UTC()).
			Where("id IN (?)", bun.In(ids)).
			Where("suspended_reason = ?", SuspendedUnverified).
			Exec(ctx)
		if err != nil {
			return err
		}
		lifted = make([]ID, len(ids))
		for i, id := range ids {
			lifted[i] = ID(id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return lifted, nil
}
