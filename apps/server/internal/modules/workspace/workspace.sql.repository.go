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
}

func (m *sqlModel) toDomain() *Workspace {
	return &Workspace{ID: ID(m.ID), Name: m.Name, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
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
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepositoryImpl{db: db}
}

func (r *SQLRepositoryImpl) Create(ctx context.Context, name string) (*Workspace, error) {
	now := time.Now().UTC()
	sm := &sqlModel{ID: uuid.NewString(), Name: name, CreatedAt: now, UpdatedAt: now}
	if _, err := dbtx.DB(ctx, r.db).NewInsert().Model(sm).Exec(ctx); err != nil {
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
		ColumnExpr("w.id AS workspace_id, w.name, w.created_at, w.updated_at, m.role, m.joined_at").
		Where("m.user_id = ?", userID).
		Order("m.joined_at", "w.id").
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	memberships := make([]Membership, len(rows))
	for i, row := range rows {
		memberships[i] = Membership{
			Workspace: Workspace{ID: ID(row.WorkspaceID), Name: row.Name, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt},
			Role:      Role(row.Role),
			JoinedAt:  row.JoinedAt,
		}
	}
	return memberships, nil
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
