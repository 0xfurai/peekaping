package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every `db` subcommand runs through the binary, as the startup script and a developer run them
func TestDBSubcommandsRunAgainstSQLite(t *testing.T) {
	tempDir := t.TempDir()
	binPath := filepath.Join(tempDir, "bun")
	build := exec.Command("go", "build", "-trimpath", "-o", binPath, "./cmd/bun")
	build.Dir = serverDirectory(t)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build bun: %v\n%s", err, output)
	}
	databasePath := filepath.Join(tempDir, "peekaping.db")
	run := func(args ...string) string {
		return runBun(t, binPath, tempDir, databasePath, append([]string{"db"}, args...)...)
	}
	expect := func(output, want string) {
		t.Helper()
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in the output, got:\n%s", want, output)
		}
	}

	run("init")
	expect(run("rollback"), "there are no groups to roll back")
	run("lock")
	run("unlock")

	// Marking applies every migration that is missing as one group, without running it
	expect(run("mark_applied"), "marked as applied group #1")
	expect(run("status"), "unapplied migrations: empty")
	expect(run("mark_applied"), "there are no new migrations to mark as applied")
	expect(run("migrate"), "there are no new migrations to run (database is up to date)")
}
