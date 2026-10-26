package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime" // aliased: this package has a runtime type
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// serverOnly are the packages the probe's binary must not link, each with why. They are the
// server's storage and queue code; a probe has no database and no Redis queue (ticket #264).
// What a monitor type's own driver needs stays: go-redis for the redis monitor, the MySQL
// driver for the mysql one, bun's pgdriver for the postgres one, and the MongoDB driver for the
// mongodb one. pgdriver imports bun's root package and its schema, which is why those are linked;
// no bun dialect is.
var serverOnly = []struct{ path, why string }{
	{"github.com/hibiken/asynq", "the server's task queue"},
	{"modernc.org/sqlite", "the SQLite driver of the server's storage"},
	{"github.com/uptrace/bun/dialect/sqlitedialect", "a bun dialect, which only the server's storage needs"},
	{"github.com/uptrace/bun/dialect/pgdialect", "a bun dialect, which only the server's storage needs"},
	{"github.com/uptrace/bun/dialect/mysqldialect", "a bun dialect, which only the server's storage needs"},
	{"github.com/uptrace/bun/driver/sqliteshim", "the SQLite driver of the server's storage"},
	{"peekaping/internal/infra", "the server's database and queue setup"},
	{"peekaping/internal/sqlitedb", "the server's SQLite setup"},
	{"peekaping/internal/dbtx", "the server's database transactions"},
	{"peekaping/internal/utils", "the server's helpers: its HTTP responses and the dig container"},
}

// modulesPrefix holds the server's modules, nearly all of which keep a repository. The probe
// links only the leaf packages in probeModules.
const modulesPrefix = "peekaping/internal/modules/"

// probeModules are the only packages of the server's modules that the probe links. Each holds no
// storage or queue: the types the modules share, the in-process event bus, the executors of the
// monitor types, what the probe's scheduler runs a check with (the job, the supervisor that runs
// one and the contract its result is ingested through), and the scheduler itself. A package that
// is not here must not get into the probe through an import; move what the probe needs of it
// into a leaf package.
var probeModules = []string{
	"check/job",
	"events",
	"healthcheck/executor",
	"healthcheck/supervisor",
	"ingester/ingest",
	"scheduler",
	"shared",
}

// goTool finds the go command the test runs: the one on PATH, or else the one of the toolchain
// that built the test
func goTool() (string, bool) {
	if path, err := exec.LookPath("go"); err == nil {
		return path, true
	}
	path := filepath.Join(goruntime.GOROOT(), "bin", "go")
	if _, err := os.Stat(path); err == nil {
		return path, true
	}
	return "", false
}

// importGraph lists the packages the main package in the current directory depends on, as the
// image build sees them (Linux, without cgo), each with the packages it imports. tags are the
// build tags of the image, such as "slim". It returns the main package's path.
func importGraph(t *testing.T, goBin, tags string) (root string, imports map[string][]string) {
	t.Helper()
	cmd := exec.Command(goBin, "list", "-tags", tags, "-deps", "-f", `{{.ImportPath}}{{"\t"}}{{.DepOnly}}{{"\t"}}{{join .Imports " "}}`, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go list -deps failed:\n%s", out)

	imports = make(map[string][]string)
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, "\t")
		require.Len(t, fields, 3, "unexpected line of go list: %q", line)
		imports[fields[0]] = strings.Fields(fields[2])
		if fields[1] == "false" {
			root = fields[0]
		}
	}
	require.NotEmpty(t, root, "go list names no main package")
	return root, imports
}

// chain is the shortest chain of imports from the root to the package, root first, or nil when
// the root does not depend on it
func chain(imports map[string][]string, root, target string) []string {
	parent := map[string]string{root: ""}
	queue := []string{root}
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		if pkg == target {
			var path []string
			for p := pkg; p != ""; p = parent[p] {
				path = append(path, p)
			}
			slices.Reverse(path)
			return path
		}
		for _, imported := range imports[pkg] {
			if _, seen := parent[imported]; !seen {
				parent[imported] = pkg
				queue = append(queue, imported)
			}
		}
	}
	return nil
}

// The probe's binary is the executors and the probe protocol's client, and nothing of the
// server's storage or queue: the image would carry it, and the process would link drivers it never
// opens. A failure names the chain of imports that brings the package in.
func TestProbeLinksNoServerStorageOrQueueCode(t *testing.T) {
	goBin, ok := goTool()
	if !ok {
		t.Skip("the go command is not available, so the probe's imports can't be listed")
	}
	root, imports := importGraph(t, goBin, "")

	// Guards against a listing that has gone empty or wrong, which would pass for nothing
	require.Contains(t, imports, "peekaping/internal/probeclient", "the probe no longer imports its client")
	require.Contains(t, imports, "peekaping/internal/modules/healthcheck/executor", "the probe no longer imports its executors")

	var problems []string
	for _, pkg := range serverOnly {
		if path := chain(imports, root, pkg.path); path != nil {
			problems = append(problems, fmt.Sprintf("%s is linked (%s):\n\t%s", pkg.path, pkg.why, strings.Join(path, "\n\t-> ")))
		}
	}
	for pkg := range imports {
		module, isModule := strings.CutPrefix(pkg, modulesPrefix)
		if !isModule || slices.Contains(probeModules, module) {
			continue
		}
		path := chain(imports, root, pkg)
		problems = append(problems, fmt.Sprintf("%s is linked, and it is not a leaf package the probe may import:\n\t%s", pkg, strings.Join(path, "\n\t-> ")))
	}
	slices.Sort(problems)
	if len(problems) > 0 {
		t.Errorf("the probe must link none of the server's storage or queue code, but it links:\n\n%s", strings.Join(problems, "\n\n"))
	}
}
