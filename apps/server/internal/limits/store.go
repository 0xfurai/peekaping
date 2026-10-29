package limits

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/modules/workspace/workspaceid"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

// SettingKey is the workspace setting that holds a workspace's own limits, as a JSON object of the
// Limits fields. The object {} is a workspace with no limits at all, whatever the install's
// defaults say; a workspace without the setting has the install's defaults.
const SettingKey = "limits"

// Store keeps the workspaces' own limits
type Store interface {
	// AllOwn returns the own limits of every workspace that has them
	AllOwn(ctx context.Context) (map[workspaceid.ID]Limits, error)
	// SetOwn gives the workspace limits of its own, which replace the install's defaults whole: an
	// empty Limits is no limits. nil removes them, and the workspace has the install's defaults
	// again.
	SetOwn(ctx context.Context, workspaceID workspaceid.ID, own *Limits) error
}

// setting is the row of workspace_settings that holds the limits
type setting struct {
	bun.BaseModel `bun:"table:workspace_settings,alias:ws"`

	WorkspaceID string    `bun:"workspace_id,pk"`
	Key         string    `bun:"key,pk"`
	Value       string    `bun:"value,notnull"`
	Type        string    `bun:"type,notnull"`
	CreatedAt   time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

// SQLStore keeps the own limits in workspace_settings. It joins the transaction ctx carries, if
// any (dbtx): a SQLite database has a single connection.
type SQLStore struct {
	db *bun.DB
}

func NewSQLStore(db *bun.DB) *SQLStore {
	return &SQLStore{db: db}
}

func (s *SQLStore) AllOwn(ctx context.Context) (map[workspaceid.ID]Limits, error) {
	var rows []setting
	if err := dbtx.DB(ctx, s.db).NewSelect().Model(&rows).Where("key = ?", SettingKey).Scan(ctx); err != nil {
		return nil, err
	}
	own := make(map[workspaceid.ID]Limits, len(rows))
	for _, row := range rows {
		var l Limits
		if err := json.Unmarshal([]byte(row.Value), &l); err != nil {
			return nil, fmt.Errorf("limits of workspace %s: %w", row.WorkspaceID, err)
		}
		own[workspaceid.ID(row.WorkspaceID)] = l.normalized()
	}
	return own, nil
}

func (s *SQLStore) SetOwn(ctx context.Context, workspaceID workspaceid.ID, own *Limits) error {
	db := dbtx.DB(ctx, s.db)
	if own == nil {
		_, err := db.NewDelete().Model((*setting)(nil)).
			Where("workspace_id = ?", string(workspaceID)).Where("key = ?", SettingKey).Exec(ctx)
		return err
	}
	value, err := json.Marshal(own.normalized())
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	row := &setting{WorkspaceID: string(workspaceID), Key: SettingKey, Value: string(value), Type: "json", CreatedAt: now, UpdatedAt: now}
	// One statement, so two writers can't both miss the row and insert it
	_, err = db.NewInsert().Model(row).
		On("CONFLICT (workspace_id, key) DO UPDATE").
		Set("value = EXCLUDED.value").
		Set("type = EXCLUDED.type").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

// LockWorkspace holds the workspace until the transaction ctx carries ends, so that a write that
// counts the workspace's records against a limit, and then adds one, sees every other such write
// of the workspace before or after it and never beside it. PostgreSQL holds the workspace's row
// with SELECT ... FOR UPDATE. SQLite has no row locks: its transactions begin with BEGIN IMMEDIATE
// (see sqlitedb.DSN), so a transaction holds the one writer for the whole database already.
// Outside a transaction it holds nothing.
func LockWorkspace(ctx context.Context, db *bun.DB, workspaceID workspaceid.ID) error {
	if db.Dialect().Name() != dialect.PG {
		return nil
	}
	var locked []string
	return dbtx.DB(ctx, db).NewSelect().Table("workspaces").Column("id").
		Where("id = ?", string(workspaceID)).For("UPDATE").Scan(ctx, &locked)
}
