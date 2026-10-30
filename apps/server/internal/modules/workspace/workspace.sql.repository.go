package workspace

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/limits"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
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
	// AdminSuspendedAt is NULL unless an instance admin holds the workspace
	AdminSuspendedAt *time.Time `bun:"admin_suspended_at"`
	// DeletionScheduledAt is NULL unless an owner scheduled the workspace's deletion
	DeletionScheduledAt *time.Time `bun:"deletion_scheduled_at"`
}

func (m *sqlModel) toDomain() *Workspace {
	return &Workspace{
		ID: ID(m.ID), Name: m.Name, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		SuspendedAt: utcOrNil(m.SuspendedAt), SuspendedReason: reasonOrEmpty(m.SuspendedReason),
		AdminSuspendedAt: utcOrNil(m.AdminSuspendedAt), DeletionScheduledAt: utcOrNil(m.DeletionScheduledAt),
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

	SuspendedAt      *time.Time `bun:"suspended_at"`
	SuspendedReason  *string    `bun:"suspended_reason"`
	AdminSuspendedAt *time.Time `bun:"admin_suspended_at"`

	DeletionScheduledAt *time.Time `bun:"deletion_scheduled_at"`
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

func (r *SQLRepositoryImpl) Rename(ctx context.Context, id ID, name string) (*Workspace, error) {
	sm := new(sqlModel)
	// RETURNING hands back the row that was updated, so there is no gap in which another rename
	// could slip in between the write and the read
	err := dbtx.DB(ctx, r.db).NewUpdate().Model(sm).
		Set("name = ?", name).
		Set("updated_at = ?", time.Now().UTC()).
		Where("w.id = ?", string(id)).
		Returning("*").
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
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
		ColumnExpr("w.id AS workspace_id, w.name, w.created_at, w.updated_at, w.suspended_at, w.suspended_reason, w.admin_suspended_at, w.deletion_scheduled_at, m.role, m.joined_at").
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
				AdminSuspendedAt: utcOrNil(row.AdminSuspendedAt), DeletionScheduledAt: utcOrNil(row.DeletionScheduledAt),
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

func (r *SQLRepositoryImpl) Delete(ctx context.Context, id ID) (*Deleted, error) {
	db := dbtx.DB(ctx, r.db)
	lock := db.NewSelect().Table("workspaces").Column("id").Where("id = ?", string(id))
	if r.db.Dialect().Name() == dialect.PG {
		lock = lock.For("UPDATE")
	}
	var found []string
	if err := lock.Scan(ctx, &found); err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	deleted := &Deleted{MonitorIDs: []string{}, UserIDs: []string{}}
	if err := db.NewSelect().Table("monitors").Column("id").Where("workspace_id = ?", string(id)).Order("id").Scan(ctx, &deleted.MonitorIDs); err != nil {
		return nil, err
	}
	if err := db.NewSelect().Table("workspace_members").Column("user_id").Where("workspace_id = ?", string(id)).Order("user_id").Scan(ctx, &deleted.UserIDs); err != nil {
		return nil, err
	}
	// The foreign keys of everything that belongs to the workspace cascade, on both databases
	if _, err := db.NewDelete().Table("workspaces").Where("id = ?", string(id)).Exec(ctx); err != nil {
		return nil, err
	}
	return deleted, nil
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

func (r *SQLRepositoryImpl) ScheduleDeletion(ctx context.Context, id ID, at time.Time) (*Workspace, bool, error) {
	db := dbtx.DB(ctx, r.db)
	// A deletion that is scheduled keeps its date
	res, err := db.NewUpdate().Table("workspaces").
		Set("deletion_scheduled_at = ?", at.UTC()).
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", string(id)).Where("deletion_scheduled_at IS NULL").Exec(ctx)
	if err != nil {
		return nil, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	found, err := r.FindByID(ctx, id)
	return found, affected > 0, err
}

func (r *SQLRepositoryImpl) CancelDeletion(ctx context.Context, id ID) (bool, bool, error) {
	db := dbtx.DB(ctx, r.db)
	now := time.Now().UTC()
	res, err := db.NewUpdate().Table("workspaces").
		Set("deletion_scheduled_at = NULL").Set("updated_at = ?", now).
		Where("id = ?", string(id)).Where("deletion_scheduled_at IS NOT NULL").Exec(ctx)
	if err != nil {
		return false, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil || affected == 0 {
		return false, false, err
	}
	var remaining bool
	if err := db.NewSelect().Table("workspaces").ColumnExpr("(suspended_at IS NOT NULL OR admin_suspended_at IS NOT NULL)").Where("id = ?", string(id)).Scan(ctx, &remaining); err != nil {
		return false, false, err
	}
	if !remaining {
		// The monitors start to run now, as a monitor that was just saved or resumed does
		if _, err := db.NewUpdate().Table("monitors").Set("updated_at = ?", now).Where("workspace_id = ?", string(id)).Exec(ctx); err != nil {
			return false, false, err
		}
	}
	return true, remaining, nil
}

func (r *SQLRepositoryImpl) ListDeletionDueAnyWorkspace(ctx context.Context, now time.Time) ([]ID, error) {
	var ids []string
	err := dbtx.DB(ctx, r.db).NewSelect().Table("workspaces").Column("id").
		Where("deletion_scheduled_at <= ?", now.UTC()).
		Order("deletion_scheduled_at", "id").
		Scan(ctx, &ids)
	if err != nil {
		return nil, err
	}
	out := make([]ID, len(ids))
	for i, id := range ids {
		out[i] = ID(id)
	}
	return out, nil
}

func (r *SQLRepositoryImpl) LockWorkspace(ctx context.Context, id ID) error {
	return limits.LockWorkspace(ctx, r.db, id)
}

func (r *SQLRepositoryImpl) LiftUnverifiedOwnedBy(ctx context.Context, userID string) ([]ID, error) {
	return r.liftUnverified(ctx, func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("id IN (SELECT workspace_id FROM workspace_members WHERE user_id = ? AND role = ?)", userID, string(RoleOwner))
	})
}

func (r *SQLRepositoryImpl) LiftUnverifiedWithVerifiedOwner(ctx context.Context, id ID) ([]ID, error) {
	return r.liftUnverified(ctx, func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("id = ?", string(id)).
			Where(`EXISTS (SELECT 1 FROM workspace_members m JOIN users u ON u.id = m.user_id
				WHERE m.workspace_id = workspaces.id AND m.role = ? AND u.email_verified_at IS NOT NULL)`, string(RoleOwner))
	})
}

// liftUnverified ends the SuspendedUnverified suspension of the workspaces that narrow selects,
// and returns them. An instance admin's hold (admin_suspended_at) is a hold of its own and stays.
func (r *SQLRepositoryImpl) liftUnverified(ctx context.Context, narrow func(*bun.SelectQuery) *bun.SelectQuery) ([]ID, error) {
	var lifted []ID
	// Read and write in one transaction, so that the IDs returned are the ones that were lifted
	err := dbtx.NewSQLTransactor(r.db).WithTx(ctx, func(ctx context.Context) error {
		q := dbtx.DB(ctx, r.db)
		var ids []string
		err := narrow(q.NewSelect().Table("workspaces").Column("id").
			Where("suspended_reason = ?", SuspendedUnverified)).
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
		// The workspace's monitors start to run now, as a monitor that was just saved or resumed does:
		// the stale-monitor watchdog counts from the later of the heartbeat and updated_at, so a monitor
		// made days ago and never checked is not stale on the first look
		_, err = q.NewUpdate().Table("monitors").
			Set("updated_at = ?", time.Now().UTC()).
			Where("workspace_id IN (?)", bun.In(ids)).
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
