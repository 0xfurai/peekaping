package apitest

import (
	"strings"
	"sync"
	"testing"

	"peekaping/internal/reporting/reportingtest"
)

// The tests of this package run in parallel: newApp calls t.Parallel for the test that builds the
// app. Run one after another they took 25 minutes under -race in CI. Each app has its own database,
// Redis and event bus, so the tests share nothing but the process. A test that changes something
// of the process, such as http.DefaultTransport or the error reporting, calls serial before it
// builds an app, and runs while no parallel test does.
//
// Subtests stay in order. A subtest that builds an app doesn't go parallel, since the test around
// it may read what the subtest did once t.Run returns.

var (
	modesMu sync.Mutex
	// modes says, for each top-level test that is running, whether it went parallel or is serial
	modes = map[string]testMode{}
)

type testMode int

const (
	parallelTest testMode = iota + 1
	serialTest
)

// parallel lets a top-level test run beside the others, unless it called serial. A test that
// builds two apps calls it twice, and t.Parallel panics when called again.
func parallel(t *testing.T) {
	t.Helper()
	if strings.Contains(t.Name(), "/") {
		return
	}
	modesMu.Lock()
	mode := modes[t.Name()]
	if mode == 0 {
		modes[t.Name()] = parallelTest
		forgetMode(t)
	}
	modesMu.Unlock()
	if mode == 0 {
		t.Parallel()
	}
}

// serial keeps the test from running beside the parallel ones, for a test that changes something
// the whole process shares. It must come before the test builds an app: once the test is parallel
// it is too late, and serial fails it.
func serial(t *testing.T) {
	t.Helper()
	top, _, sub := strings.Cut(t.Name(), "/")
	modesMu.Lock()
	defer modesMu.Unlock()
	switch modes[top] {
	case parallelTest:
		t.Fatalf("%s changes the process and must run alone: call serial before newApp, which runs the test in parallel", top)
	case 0:
		// A subtest runs in order inside its test, which is serial unless it built an app
		if !sub {
			modes[top] = serialTest
			forgetMode(t)
		}
	}
}

// forgetMode drops the test's mode when it ends, for -count to run it again. The caller holds modesMu.
func forgetMode(t *testing.T) {
	t.Cleanup(func() {
		modesMu.Lock()
		defer modesMu.Unlock()
		delete(modes, t.Name())
	})
}

// installReporting is reportingtest.Install for a test of this package. Error reporting is the
// process's, so the test runs alone.
func installReporting(t *testing.T) *reportingtest.Recorder {
	t.Helper()
	serial(t)
	return reportingtest.Install(t)
}
