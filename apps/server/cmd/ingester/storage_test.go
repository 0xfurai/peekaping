package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runMainEnv names the environment variable that has the test binary run main instead of the tests
const runMainEnv = "PEEKAPING_TEST_RUN_MAIN"

// TestHelperProcess_Main is main run in a process of its own, for the tests below to start
func TestHelperProcess_Main(t *testing.T) {
	if os.Getenv(runMainEnv) != "1" {
		t.Skip("runs only as the process another test starts")
	}
	main()
}

// startMain runs the ingester's main with the environment, and returns what it printed and how it
// ended
func startMain(t *testing.T, env ...string) (output string, exitCode int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess_Main$")
	cmd.Env = append(os.Environ(), append([]string{runMainEnv + "=1", "MODE=test", "REDIS_HOST=127.0.0.1", "REDIS_PORT=1"}, env...)...)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return string(out), exit.ExitCode()
	}
	require.NoError(t, err, string(out))
	return string(out), 0
}

// AC6: on SQLite and PostgreSQL the api stores what the probes send and what the push URLs take,
// so the ingester refuses to start, and points at the upgrade steps
func TestMain_TheIngesterRefusesToStartOnSQLStorage(t *testing.T) {
	for name, env := range map[string][]string{
		"sqlite": {"DB_TYPE=sqlite", "DB_NAME=" + filepath.Join(t.TempDir(), "peekaping.db")},
		"postgres": {
			"DB_TYPE=postgres", "DB_HOST=127.0.0.1", "DB_PORT=1", "DB_USER=peekaping", "DB_PASS=secret", "DB_NAME=peekaping",
		},
	} {
		t.Run(name, func(t *testing.T) {
			output, code := startMain(t, env...)

			assert.Equal(t, 1, code, "the ingester exits with an error")
			assert.Contains(t, output, "the ingester no longer runs on SQLite or PostgreSQL storage")
			assert.Contains(t, output, "https://docs.peekaping.com/self-hosting/upgrading-to-probes")
			assert.Contains(t, output, "the api stores the results")
			assert.NotContains(t, output, "Starting the ingester", "and starts nothing")
		})
	}
}

// MongoDB has no probes (ADR 0001): its ingester runs as it always did, so its config still
// validates and its storage is not refused
func TestCheckStorage_TheIngesterStillRunsOnMongoDB(t *testing.T) {
	for _, dbType := range []string{"mongo", "mongodb"} {
		t.Run(dbType, func(t *testing.T) {
			t.Setenv("DB_TYPE", dbType)
			t.Setenv("DB_HOST", "mongo")
			t.Setenv("DB_PORT", "27017")
			t.Setenv("DB_NAME", "peekaping")
			t.Setenv("DB_USER", "peekaping")
			t.Setenv("DB_PASS", "secret")

			cfg, err := LoadAndValidate(t.TempDir())

			require.NoError(t, err, "the config validates as before")
			assert.NoError(t, checkStorage(cfg))
		})
	}
}
