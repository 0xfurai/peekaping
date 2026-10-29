package internal_test

// A method whose name ends in AnyWorkspace reads or writes across workspaces (docs/adr/0005 section 3).
// Only the paths the ADR lists may call one, and a new caller needs a reviewed change to the list
// below (PP-153, AC3). The test fails both ways: a caller that isn't in the list, and an entry that no
// call matches, so the list can't hold a path that was removed.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const anyWorkspaceSuffix = "AnyWorkspace"

// anyWorkspaceCallers are the functions that call a method named ...AnyWorkspace, as
// "<directory from apps/server>.<type>.<function>" (a function with no receiver has no type), each
// with the path of ADR 0005 section 3 it serves. The tickets that scoped each path are PP-148 (monitors),
// PP-149 (channels, proxies, tags, maintenance windows), PP-150 (status pages, settings, probes) and
// PP-151 (the websocket); API keys are PP-147 and invites PP-154.
//
// A function whose own name ends in AnyWorkspace is not a caller: it passes the call on, and its
// own callers are listed here instead.
var anyWorkspaceCallers = map[string]string{
	// push endpoint
	"internal/modules/healthcheck.RegisterPushEndpoint": "the push endpoint finds a monitor by its push token, and the token alone says which workspace",
	// public status pages and badges
	"internal/modules/status_page.Controller.FindBySlug":      "a visitor of a public status page is in no workspace; the slug is unique across the install",
	"internal/modules/status_page.Controller.FindByDomain":    "a visitor of a custom domain is in no workspace; the domain is unique across the install",
	"internal/modules/status_page.Controller.publicMonitors":  "the monitors of a published page, loaded for the visitor",
	"internal/modules/status_page.Controller.publicLocations": "the heartbeats and uptime of a monitor on a published page",
	"internal/modules/status_page.ServiceImpl.validateSlug":   "slugs are unique across the install, so a new page checks every workspace's",
	"internal/modules/badge.ServiceImpl.GetMonitorBadgeData":  "a badge is public for a monitor on a published page, whatever its workspace",
	"internal/modules/badge.ServiceImpl.IsMonitorPublic":      "a badge is served only for a monitor on a published page",
	"internal/modules/badge.ServiceImpl.ResolveMonitorID":     "a badge finds its monitor by ID with no workspace",
	"internal/modules/badge.ServiceImpl.getMonitorBasicInfo":  "a badge shows a published monitor's name and status",
	// probe protocol
	"internal/modules/probe/protocol.Controller.assign":            "the probe protocol deals the monitors of every workspace to the probe's instances",
	"internal/modules/probe/protocol.Controller.dealtMonitors":     "a probe's results are accepted for the monitors it was dealt, whatever their workspace",
	"internal/modules/probe/protocol.Controller.otherMonitor":      "a probe's result for a monitor it wasn't dealt is refused after looking the monitor up",
	"internal/modules/probe/protocol.Controller.assignAllMonitors": "a probe's first hello assigns it the monitors of the install",
	"internal/app.probeMonitorAssigner.AssignProbeToAll":           "an instance admin adds a probe, which is the install's, to every monitor (ADR 0005 section 10)",
	// cluster
	"internal/app.provideSilentWatch": "cluster mode: the sweep asks whether the local probe has monitors to run in any workspace",
	// scheduler, check loader, watchdog
	"internal/modules/scheduler.DatabaseSource.Find":       "the scheduler loads the monitor of a job, whatever its workspace",
	"internal/modules/scheduler.DatabaseSource.ListActive": "the scheduler lists the active monitors of every workspace",
	"internal/modules/check.Loader.Load":                   "a check loads its monitor by ID, whatever its workspace",
	"internal/modules/check.Loader.isUnderMaintenance":     "a check looks up the maintenance windows of its monitor",
	"internal/modules/healthcheck.MonitorUnderMaintenance": "a push checks the maintenance windows of its monitor",
	"internal/modules/stale.Watchdog.checkStaleMonitors":   "the watchdog looks for stale monitors in every workspace",
	// ingester
	"internal/modules/ingester.ServiceImpl.Ingest":          "the ingester stores a probe's result on the monitor, whatever its workspace",
	"internal/modules/ingester.ServiceImpl.restoreLocation": "the ingester restores a monitor's location state after a failed result",
	// monitor event listener, status repair
	"internal/modules/monitor.MonitorEventListener.handleMonitorStatusChanged": "the monitor event listener stores a status change by the monitor's ID",
	"internal/modules/monitor.MonitorServiceImpl.find":                         "the monitor service's one lookup serves a request (with its workspace) and a background job (without)",
	"internal/modules/monitor.MonitorServiceImpl.updatePartial":                "a background update (no workspace) changes a status and never links",
	"internal/modules/monitor.MonitorServiceImpl.repairStoredStatus":           "status repair at startup repairs the monitors of every workspace",
	"cmd/api.startJobs":       "startup repair of stored statuses covers every workspace",
	"cmd/peekaping.startJobs": "startup repair of stored statuses covers every workspace",
	// notification listener
	"internal/modules/notification_channel.NotificationEventListener.channelsByID":           "a notification goes to the channels linked to the monitor",
	"internal/modules/notification_channel.NotificationEventListener.findChannels":           "a notification goes to the channels linked to the monitor",
	"internal/modules/notification_channel.NotificationEventListener.probeMonitorChannelIDs": "a probe's offline notification goes to the channels linked to it",
	// metrics
	"internal/modules/metrics.Collector.handleHeartbeat": "/metrics lists every monitor of the install, for the people who run it",
	"internal/modules/metrics.Collector.sync":            "/metrics lists every monitor of the install, for the people who run it",
	// telemetry
	"internal/modules/telemetry.ServiceImpl.build": "the usage ping counts the install's monitors, channels and pages",
	// cleanup
	"internal/modules/cleanup.cleanupHeartbeats":         "the cleanup job goes workspace by workspace, each with its own retention period",
	"internal/modules/cleanup.cleanupProbeStatusChanges": "the cleanup job lists every workspace to find the longest retention period",
	// websocket
	"internal/modules/websocket.Server.workspaceOf": "a heartbeat goes to the room of its monitor's workspace",
	// API keys
	"internal/modules/api_key.ServiceImpl.findForToken": "an API key is found by the ID in its token, which then says its workspace",
	// admin area
	"internal/modules/admin.SQLRepository.FindUser":            "the admin area shows each user's workspaces, by name and role, whichever they are in; an instance admin sees no workspace's records",
	"internal/modules/admin.ServiceImpl.ListUsers":             "the admin area lists each user with their workspaces, by name and role; an instance admin sees no workspace's records",
	"internal/modules/admin.ServiceImpl.DeleteUser":            "deleting a user checks whether they are the last owner of any workspace they are in, and refuses naming it",
	"internal/modules/admin.ServiceImpl.ListWorkspaces":        "the admin area lists every workspace of the install with counts and metadata only; an instance admin sees no workspace's records (PP-372)",
	"internal/modules/admin.ServiceImpl.SetWorkspaceSuspended": "an instance admin suspends or resumes a workspace without joining it, and gets its counts and metadata back; no workspace's records are read (PP-372)",
	"internal/modules/admin.ServiceImpl.DeleteWorkspace":       "an instance admin deletes a workspace without joining it; it goes with everything in it, by cascade, and no workspace's records are read (PP-372)",
	// invites
	"internal/modules/workspace.ServiceImpl.lookupInvite": "an invite is found by the hash of its token, which then says its workspace, as an API key's does (PP-154)",
}

// anyWorkspaceCall is one reference to a method named ...AnyWorkspace
type anyWorkspaceCall struct {
	caller string
	file   string
}

// findAnyWorkspaceCalls parses the Go files of apps/server, test files left out, and returns the
// functions that call (or take as a value) a method whose name ends in AnyWorkspace
func findAnyWorkspaceCalls(t *testing.T, root string) []anyWorkspaceCall {
	t.Helper()
	var found []anyWorkspaceCall
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == "node_modules" || strings.HasPrefix(name, ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(relative))
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Body == nil || strings.HasSuffix(d.Name.Name, anyWorkspaceSuffix) {
					continue
				}
				name := d.Name.Name
				if d.Recv != nil && len(d.Recv.List) > 0 {
					name = receiverName(d.Recv.List[0].Type) + "." + name
				}
				for range referencesIn(d.Body) {
					found = append(found, anyWorkspaceCall{caller: dir + "." + name, file: filepath.ToSlash(relative)})
				}
			case *ast.GenDecl:
				// a function literal in a package-level variable
				for _, spec := range d.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for range referencesIn(value) {
						found = append(found, anyWorkspaceCall{caller: dir + ".var " + value.Names[0].Name, file: filepath.ToSlash(relative)})
					}
				}
			}
		}
		return nil
	}))
	return found
}

func receiverName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if index, ok := expr.(*ast.IndexExpr); ok {
		expr = index.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return "?"
}

// referencesIn returns the names ending in AnyWorkspace that the node calls or refers to as a
// method (x.NameAnyWorkspace) or as a function of its own package (NameAnyWorkspace(...))
func referencesIn(node ast.Node) []string {
	var names []string
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if strings.HasSuffix(x.Sel.Name, anyWorkspaceSuffix) {
				names = append(names, x.Sel.Name)
			}
		case *ast.CallExpr:
			if ident, ok := x.Fun.(*ast.Ident); ok && strings.HasSuffix(ident.Name, anyWorkspaceSuffix) {
				names = append(names, ident.Name)
			}
		}
		return true
	})
	return names
}

// Only the paths ADR 0005 section 3 lists call a method that works across workspaces (AC3)
func TestAnyWorkspaceMethodsAreCalledOnlyFromTheListedPaths(t *testing.T) {
	found := map[string]bool{}
	for _, call := range findAnyWorkspaceCalls(t, "..") {
		found[call.caller] = true
	}

	var unlisted, stale []string
	for caller := range found {
		if _, listed := anyWorkspaceCallers[caller]; !listed {
			unlisted = append(unlisted, caller)
		}
	}
	for caller, reason := range anyWorkspaceCallers {
		assert.NotEmpty(t, strings.TrimSpace(reason), "%s has no reason", caller)
		if !found[caller] {
			stale = append(stale, caller)
		}
	}
	sort.Strings(unlisted)
	sort.Strings(stale)
	assert.Empty(t, unlisted, "these call a method named ...AnyWorkspace, which reads across workspaces and is for the paths of docs/adr/0005 section 3 only. If the path belongs there, add it to anyWorkspaceCallers in internal/isolation_test.go with its reason, in a reviewed change; if not, use the method that takes the workspace")
	assert.Empty(t, stale, "these are in anyWorkspaceCallers and no longer call a method named ...AnyWorkspace: remove them, or fix a renamed function's entry")
}

// The finder sees what it is meant to: a call through a selector, a call of a function of the
// same package, a method value and a function literal in a package-level variable, and not a test
// file or a function that only passes the call on
func TestAnyWorkspaceFinderSeesEveryKindOfReference(t *testing.T) {
	dir := t.TempDir()
	write := func(name, source string) {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(source), 0o644))
	}
	write("pkg/a.go", `package pkg

type S struct{ r R }
type R interface{ FindAnyWorkspace() }

func (s *S) ViaSelector()   { s.r.FindAnyWorkspace() }
func (s *S) AsValue()       { f := s.r.FindAnyWorkspace; f() }
func SameName()             { ListAnyWorkspace() }
func ListAnyWorkspace()     {}
func (s *S) PassOnAnyWorkspace() { s.r.FindAnyWorkspace() }

var job = func() { new(S).r.FindAnyWorkspace() }
`)
	write("pkg/a_test.go", `package pkg

func inATest() { new(S).r.FindAnyWorkspace() }
`)

	var callers []string
	for _, call := range findAnyWorkspaceCalls(t, dir) {
		callers = append(callers, call.caller)
	}

	assert.ElementsMatch(t, []string{"pkg.S.ViaSelector", "pkg.S.AsValue", "pkg.SameName", "pkg.var job"}, callers)
}
