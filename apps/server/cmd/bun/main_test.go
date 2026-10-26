package main

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/uptrace/bun/driver/sqliteshim"
)

func TestTrimpathBinaryRunsWithoutMigrationSources(t *testing.T) {
	serverDir := serverDirectory(t)
	tempDir := t.TempDir()
	binPath := filepath.Join(tempDir, "bun")

	build := exec.Command("go", "build", "-trimpath", "-o", binPath, "./cmd/bun")
	build.Dir = serverDir
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build bun with -trimpath: %v\n%s", err, output)
	}

	runtimeDir := filepath.Join(tempDir, "runtime")
	if err := os.Mkdir(runtimeDir, 0o755); err != nil {
		t.Fatalf("create runtime directory: %v", err)
	}
	databasePath := filepath.Join(runtimeDir, "peekaping.db")

	runBun(t, binPath, runtimeDir, databasePath, "db", "init")
	runBun(t, binPath, runtimeDir, databasePath, "db", "migrate")
	status := runBun(t, binPath, runtimeDir, databasePath, "db", "status")
	if !strings.Contains(status, "unapplied migrations: empty") {
		t.Fatalf("expected every embedded migration to be applied, got:\n%s", status)
	}
	assertAppliedMigrations(t, databasePath)
	if _, err := os.Stat(filepath.Join(runtimeDir, "cmd", "bun", "migrations")); !os.IsNotExist(err) {
		t.Fatalf("runtime unexpectedly contains migration sources: %v", err)
	}

	developmentDir := filepath.Join(tempDir, "development", "apps", "server")
	migrationsDir := filepath.Join(developmentDir, "cmd", "bun", "migrations")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		t.Fatalf("create development migration directory: %v", err)
	}
	developmentDatabase := filepath.Join(developmentDir, "peekaping.db")

	runBun(t, binPath, developmentDir, developmentDatabase, "db", "create_go", "portable_go")
	runBun(t, binPath, developmentDir, developmentDatabase, "db", "create_sql", "portable_sql")
	runBun(t, binPath, developmentDir, developmentDatabase, "db", "create_tx_sql", "portable_tx_sql")

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("read created migrations: %v", err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	for _, suffix := range []string{
		"_portable_go.go",
		"_portable_sql.up.sql",
		"_portable_sql.down.sql",
		"_portable_tx_sql.tx.up.sql",
		"_portable_tx_sql.tx.down.sql",
	} {
		if !slices.ContainsFunc(names, func(name string) bool { return strings.HasSuffix(name, suffix) }) {
			t.Errorf("migration ending in %q was not created in %s; got %v", suffix, migrationsDir, names)
		}
	}
}

func serverDirectory(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get test working directory: %v", err)
	}
	for {
		_, err := os.Stat(filepath.Join(dir, "go.mod"))
		if err == nil {
			return dir
		}
		if !os.IsNotExist(err) {
			t.Fatalf("inspect %s for go.mod: %v", dir, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("find apps/server module from test working directory %s", dir)
		}
		dir = parent
	}
}

func assertAppliedMigrations(t *testing.T, databasePath string) {
	t.Helper()
	db, err := sql.Open(sqliteshim.ShimName, "file:"+databasePath+"?mode=ro")
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var total int
	if err := db.QueryRow("SELECT COUNT(*) FROM bun_migrations").Scan(&total); err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}
	if total != 24 {
		t.Fatalf("expected 22 embedded SQL migrations and two Go migrations, got %d applied migrations", total)
	}

	for _, name := range []string{"20261017120100", "20261028120200"} {
		var goMigrationCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM bun_migrations WHERE name = ?", name).Scan(&goMigrationCount); err != nil {
			t.Fatalf("check applied Go migration: %v", err)
		}
		if goMigrationCount != 1 {
			t.Fatalf("expected Go migration %s to be applied once, got %d", name, goMigrationCount)
		}
	}
}

func runBun(t *testing.T, binPath, workingDir, databasePath string, args ...string) string {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Dir = workingDir
	cmd.Env = append(withoutEnvironment(os.Environ(), "DB_TYPE", "DB_NAME"),
		"DB_TYPE=sqlite",
		"DB_NAME="+databasePath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bun %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func withoutEnvironment(environment []string, keys ...string) []string {
	filtered := make([]string, 0, len(environment))
	for _, variable := range environment {
		key, _, _ := strings.Cut(variable, "=")
		if !slices.Contains(keys, key) {
			filtered = append(filtered, variable)
		}
	}
	return filtered
}
