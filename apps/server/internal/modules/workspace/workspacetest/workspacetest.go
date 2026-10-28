// Package workspacetest helps tests that write rows of a workspace's tables with raw SQL, in a
// database the migrations built.
package workspacetest

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

// ID is the workspace that Ensure makes in a database that has none
const ID = workspace.ID("0d6f1e0e-6b1c-4d49-8c37-0b5f4a1b3a01")

// Other is a second workspace, which Add makes
const Other = workspace.ID("6a1c9d52-3b7e-4f0a-9d1e-5c2b8e7f4a02")

// Ensure returns the ID of the database's oldest workspace, and makes the workspace ID when it has
// none. A raw
// INSERT into a table that belongs to a workspace names it as
// (SELECT id FROM workspaces ORDER BY created_at, id LIMIT 1), which is the workspace Ensure
// returns, the one the upgrade gives an install's records.
func Ensure(t testing.TB, db bun.IDB) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := db.NewSelect().Table("workspaces").Column("id").Order("created_at", "id").Limit(1).Scan(ctx, &id)
	if err == nil {
		return id
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("find the oldest workspace: %v", err)
	}
	Add(t, db, ID, "Test")
	return string(ID)
}

// Add makes a workspace with the ID
func Add(t testing.TB, db bun.IDB, id workspace.ID, name string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), "INSERT INTO workspaces (id, name) VALUES (?, ?)", string(id), name); err != nil {
		t.Fatalf("make a workspace: %v", err)
	}
}

// Prepare makes sure the install has what a monitor needs to be stored and then upgraded, in
// whatever state the database is in. A database that has workspaces gets Ensure's workspace. An
// older one, from before there were any, gets a user when it has none, so that the upgrade finds
// an install to give a workspace to.
func Prepare(t testing.TB, db *bun.DB) {
	t.Helper()
	ctx := context.Background()
	if hasTable(t, db, "workspaces") {
		Ensure(t, db)
		return
	}
	var users int
	if err := db.NewSelect().Table("users").ColumnExpr("COUNT(*)").Scan(ctx, &users); err != nil {
		t.Fatalf("count the users: %v", err)
	}
	if users == 0 {
		if _, err := db.ExecContext(ctx, "INSERT INTO users (id, email, password) VALUES (?, 'first@example.com', 'hash')", uuid.NewString()); err != nil {
			t.Fatalf("add a user: %v", err)
		}
	}
}

// InsertMonitor stores a monitor with the given ID, in whatever state the database is in (see
// Prepare). Where monitors have a workspace, it belongs to Ensure's.
func InsertMonitor(t testing.TB, db *bun.DB, id string) {
	t.Helper()
	Prepare(t, db)
	const columns = "type, name, interval, timeout, max_retries, retry_interval, resend_interval"
	const values = "'http', 'Monitor', 60, 10, 1, 10, 0"
	statement := "INSERT INTO monitors (id, " + columns + ") VALUES (?, " + values + ")"
	if hasColumn(t, db, "monitors", "workspace_id") {
		statement = "INSERT INTO monitors (id, workspace_id, " + columns + ") VALUES (?, (SELECT id FROM workspaces ORDER BY created_at, id LIMIT 1), " + values + ")"
	}
	if _, err := db.ExecContext(context.Background(), statement, id); err != nil {
		t.Fatalf("insert a monitor: %v", err)
	}
}

func hasTable(t testing.TB, db *bun.DB, table string) bool {
	t.Helper()
	var count int
	var err error
	if db.Dialect().Name() == dialect.SQLite {
		err = db.NewRaw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(context.Background(), &count)
	} else {
		err = db.NewRaw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?", table).Scan(context.Background(), &count)
	}
	if err != nil {
		t.Fatalf("look for the %s table: %v", table, err)
	}
	return count > 0
}

func hasColumn(t testing.TB, db *bun.DB, table, column string) bool {
	t.Helper()
	var count int
	var err error
	if db.Dialect().Name() == dialect.SQLite {
		err = db.NewRaw("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?", table, column).Scan(context.Background(), &count)
	} else {
		err = db.NewRaw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?", table, column).Scan(context.Background(), &count)
	}
	if err != nil {
		t.Fatalf("look for the %s column: %v", column, err)
	}
	return count > 0
}
