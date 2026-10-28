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

// ID is the workspace that OldestOrAdd adds in a database that has none
const ID = workspace.ID("0d6f1e0e-6b1c-4d49-8c37-0b5f4a1b3a01")

// Other is a second workspace, which Add makes
const Other = workspace.ID("6a1c9d52-3b7e-4f0a-9d1e-5c2b8e7f4a02")

// OldestOrAdd returns the ID of the database's oldest workspace by created_at, and adds the
// workspace ID (named "Test") when the database has none. A raw INSERT into a table that belongs
// to a workspace names it as (SELECT id FROM workspaces ORDER BY created_at, id LIMIT 1), which
// is the workspace OldestOrAdd returns, the one the upgrade gives an install's records. A test
// that cares which workspace a row is in adds its own with Add instead.
func OldestOrAdd(t testing.TB, db bun.IDB) string {
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

// InsertMonitor stores a monitor with the given ID in the oldest workspace (see OldestOrAdd). It
// fails the test on a database whose monitors have no workspace_id column yet, which needs
// InsertMonitorWithoutWorkspace.
func InsertMonitor(t testing.TB, db *bun.DB, id string) {
	t.Helper()
	if !hasColumn(t, db, "monitors", "workspace_id") {
		t.Fatal("the monitors table has no workspace_id column yet: this schema is from before the monitors had a workspace, so use InsertMonitorWithoutWorkspace")
	}
	OldestOrAdd(t, db)
	const statement = "INSERT INTO monitors (id, workspace_id, " + monitorColumns + ") VALUES (?, (SELECT id FROM workspaces ORDER BY created_at, id LIMIT 1), " + monitorValues + ")"
	if _, err := db.ExecContext(context.Background(), statement, id); err != nil {
		t.Fatalf("insert a monitor: %v", err)
	}
}

// InsertMonitorWithoutWorkspace stores a monitor with the given ID in a database from before the
// monitors had a workspace: one whose monitors table has no workspace_id column, as a test that
// stops the migrations short of the one that adds it. It fails the test on a database that has the
// column, which needs InsertMonitor. It does PrepareUpgrade first.
func InsertMonitorWithoutWorkspace(t testing.TB, db *bun.DB, id string) {
	t.Helper()
	if hasColumn(t, db, "monitors", "workspace_id") {
		t.Fatal("the monitors table has a workspace_id column: use InsertMonitor")
	}
	PrepareUpgrade(t, db)
	const statement = "INSERT INTO monitors (id, " + monitorColumns + ") VALUES (?, " + monitorValues + ")"
	if _, err := db.ExecContext(context.Background(), statement, id); err != nil {
		t.Fatalf("insert a monitor: %v", err)
	}
}

// oldest is the SQL that names the oldest workspace, which OldestOrAdd returns
const oldest = "(SELECT id FROM workspaces ORDER BY created_at, id LIMIT 1)"

// InsertChannel stores a notification channel with the given ID in the oldest workspace (see
// OldestOrAdd)
func InsertChannel(t testing.TB, db bun.IDB, id string) {
	t.Helper()
	OldestOrAdd(t, db)
	if _, err := db.ExecContext(context.Background(), "INSERT INTO notification_channels (id, workspace_id, name, type) VALUES (?, "+oldest+", 'Ops', 'webhook')", id); err != nil {
		t.Fatalf("insert a notification channel: %v", err)
	}
}

// InsertTag stores a tag with the given ID and name in the oldest workspace (see OldestOrAdd)
func InsertTag(t testing.TB, db bun.IDB, id, name string) {
	t.Helper()
	OldestOrAdd(t, db)
	if _, err := db.ExecContext(context.Background(), "INSERT INTO tags (id, workspace_id, name, color) VALUES (?, "+oldest+", ?, '#3B82F6')", id, name); err != nil {
		t.Fatalf("insert a tag: %v", err)
	}
}

// InsertProxy stores a proxy with the given ID in the oldest workspace (see OldestOrAdd)
func InsertProxy(t testing.TB, db bun.IDB, id string) {
	t.Helper()
	OldestOrAdd(t, db)
	if _, err := db.ExecContext(context.Background(), "INSERT INTO proxies (id, workspace_id, protocol, host, port) VALUES (?, "+oldest+", 'http', 'localhost', 8080)", id); err != nil {
		t.Fatalf("insert a proxy: %v", err)
	}
}

// InsertMaintenance stores a maintenance window with the given ID in the oldest workspace (see
// OldestOrAdd)
func InsertMaintenance(t testing.TB, db bun.IDB, id string) {
	t.Helper()
	OldestOrAdd(t, db)
	if _, err := db.ExecContext(context.Background(), "INSERT INTO maintenances (id, workspace_id, title, strategy) VALUES (?, "+oldest+", 'Upgrade', 'manual')", id); err != nil {
		t.Fatalf("insert a maintenance window: %v", err)
	}
}

// PrepareUpgrade gives a database from before the monitors had a workspace what the upgrade needs
// to find: the oldest workspace when the database has workspaces already (see OldestOrAdd), and
// otherwise a user, when it has none, so that the upgrade finds an install to give a workspace to.
func PrepareUpgrade(t testing.TB, db *bun.DB) {
	t.Helper()
	ctx := context.Background()
	if hasTable(t, db, "workspaces") {
		OldestOrAdd(t, db)
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

const (
	monitorColumns = "type, name, interval, timeout, max_retries, retry_interval, resend_interval"
	monitorValues  = "'http', 'Monitor', 60, 10, 1, 10, 0"
)

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
