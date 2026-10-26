package reporting_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"

	"peekaping/internal/reporting"
	"peekaping/internal/reporting/reportingtest"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// panicked runs fn and returns what it panicked with, and the stack taken while the panic was in
// flight, as a hook that recovers a panic takes it
func panicked(fn func()) (value any, stack []byte) {
	defer func() {
		value = recover()
		stack = debug.Stack()
	}()
	fn()
	return nil, nil
}

// explodeLine is the line explode panics on, as the runtime reports it
var explodeLine int

//go:noinline
func explode(msg any) {
	_, _, n, _ := runtime.Caller(0)
	explodeLine = n + 2 // the panic is two lines below this call
	panic(msg)
}

//go:noinline
func outOfRange(i int) int {
	s := []int{1, 2, 3}
	return s[i]
}

func TestCapturePanic_ReportsTheTypeTheFramesTheReleaseAndTheTags(t *testing.T) {
	recorder := reportingtest.Install(t)
	value, stack := panicked(func() { explode(errors.New("boom")) })

	reporting.CapturePanic("http", value, stack)

	events := recorder.Events()
	require.Len(t, events, 1)
	event := events[0]
	assert.Equal(t, "go", event.Platform)
	assert.Equal(t, sentry.LevelError, event.Level)
	assert.Equal(t, reportingtest.Release, event.Release)
	assert.Equal(t, map[string]string{
		"mode":      reportingtest.Mode,
		"db":        reportingtest.DB,
		"os":        runtime.GOOS + "/" + runtime.GOARCH,
		"component": "http",
	}, event.Tags)

	require.Len(t, event.Exception, 1)
	exception := event.Exception[0]
	assert.Equal(t, "*errors.errorString", exception.Type)
	assert.Empty(t, exception.Value, "the value can hold anything the panic's message holds")
	require.NotNil(t, exception.Stacktrace)
	frames := exception.Stacktrace.Frames
	require.NotEmpty(t, frames)

	// The oldest frame comes first, as Sentry wants it, and the panic is the last one
	last := frames[len(frames)-1]
	assert.Equal(t, "explode", last.Function)
	assert.Equal(t, "peekaping/internal/reporting_test", last.Module)
	assert.Equal(t, "peekaping/internal/reporting_test/reporting_test.go", last.Filename)
	assert.Equal(t, explodeLine, last.Lineno)
	assert.True(t, last.InApp)
	for _, frame := range frames {
		assert.Empty(t, frame.AbsPath, "an absolute path names the machine that built the binary")
		assert.NotContains(t, frame.Filename, "/Users/")
		assert.NotEqual(t, "panic", frame.Function, "frames before the panic are the hook's own")
		assert.NotContains(t, frame.Function, "debug.Stack")
	}
	callers := functions(frames)
	assert.Contains(t, callers, "TestCapturePanic_ReportsTheTypeTheFramesTheReleaseAndTheTags.func1")
	assert.Contains(t, callers, "panicked")
}

// functions are the function names of frames
func functions(frames []sentry.Frame) []string {
	names := make([]string, 0, len(frames))
	for _, f := range frames {
		names = append(names, f.Function)
	}
	return names
}

func TestCapturePanic_ReportsARuntimeErrorFromItsCaller(t *testing.T) {
	recorder := reportingtest.Install(t)
	value, stack := panicked(func() { outOfRange(7) })

	reporting.CapturePanic("scheduler", value, stack)

	require.Len(t, recorder.Events(), 1)
	exception := recorder.Events()[0].Exception[0]
	assert.Equal(t, "runtime.boundsError", exception.Type)
	frames := exception.Stacktrace.Frames
	assert.Equal(t, "outOfRange", frames[len(frames)-1].Function, "the runtime's own frames are cut")
	for _, frame := range frames[len(frames)-1:] {
		assert.NotEqual(t, "runtime", frame.Module)
	}
}

// secrets are what the text of an error can hold and a report must never carry: a monitor URL, an
// Authorization header value and a Slack webhook URL
var secrets = []string{
	"https://secret.example.com/health?token=abc",
	"secret.example.com",
	"Bearer s3cr3t",
	"s3cr3t",
	"https://hooks.slack.com/services/T000/B000/XXXX",
	"XXXX",
}

// errorHoldingSecrets is an error whose text holds every one of secrets
func errorHoldingSecrets() error {
	return fmt.Errorf("GET %s failed: Authorization: Bearer s3cr3t, notify %s",
		"https://secret.example.com/health?token=abc", "https://hooks.slack.com/services/T000/B000/XXXX")
}

func TestBeforeSend_DropsUserData(t *testing.T) {
	recorder := reportingtest.Install(t)
	value, stack := panicked(func() { explode(errorHoldingSecrets()) })

	reporting.CapturePanic("executor", value, stack)

	require.Len(t, recorder.Events(), 1)
	sent, err := json.Marshal(recorder.Events()[0])
	require.NoError(t, err)
	for _, secret := range secrets {
		assert.NotContains(t, string(sent), secret)
	}
	if hostname, err := os.Hostname(); err == nil && len(hostname) >= 4 {
		assert.NotContains(t, string(sent), hostname)
	}
	assert.Empty(t, recorder.Events()[0].ServerName, "Sentry fills the server name with the hostname")
	assert.Contains(t, string(sent), `"type":"*errors.errorString"`)
	assert.Contains(t, string(sent), `"release":"`+reportingtest.Release+`"`)
	assert.Contains(t, string(sent), `"function":"explode"`)
	assert.Contains(t, string(sent), `"mode":"single"`)
	assert.Contains(t, string(sent), `"db":"sqlite"`)
	assert.Contains(t, string(sent), `"os":"`+runtime.GOOS+"/"+runtime.GOARCH+`"`)
}

// Sentry may add fields to its event, or fill the ones it has before BeforeSend runs. The event
// that goes out is built from the allowlist, so none of that goes with it.
func TestBeforeSend_BuildsAFreshEventFromTheAllowlist(t *testing.T) {
	reportingtest.Install(t)
	polluted := &sentry.Event{
		EventID:     "0123456789abcdef0123456789abcdef",
		Timestamp:   time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
		Level:       sentry.LevelFatal,
		Platform:    "go",
		Message:     "dial tcp 10.0.0.5:5432: connect: refused",
		ServerName:  "db-prod-1",
		Release:     "someone else's release",
		Dist:        "dist",
		Environment: "production",
		Transaction: "GET /api/v1/monitors/secret-id",
		Logger:      "logger",
		Fingerprint: []string{"secret"},
		Modules:     map[string]string{"github.com/acme/private": "v1"},
		Contexts:    map[string]sentry.Context{"device": {"hostname": "db-prod-1"}},
		Breadcrumbs: []*sentry.Breadcrumb{{Message: "query SELECT secret"}},
		User:        sentry.User{Email: "admin@example.com", IPAddress: "203.0.113.9"},
		Request:     &sentry.Request{URL: "https://secret.example.com/", Headers: map[string]string{"Authorization": "Bearer s3cr3t"}},
		Threads:     []sentry.Thread{{Name: "main"}},
		Tags:        map[string]string{"component": "http", "customer": "acme"},
		Exception: []sentry.Exception{{
			Type:  "*pq.Error",
			Value: "password authentication failed for user acme",
			Stacktrace: &sentry.Stacktrace{Frames: []sentry.Frame{{
				Function: "Query", Module: "peekaping/internal/db", Filename: "peekaping/internal/db/db.go", Lineno: 42, InApp: true,
				AbsPath: "/Users/alice/src/db.go", ContextLine: "password := os.Getenv(\"SECRET\")", Vars: map[string]any{"password": "hunter2"},
			}}},
			Mechanism: &sentry.Mechanism{Type: "generic"},
		}},
	}

	got := reporting.BeforeSend(polluted)

	require.NotNil(t, got)
	allowed := map[string]bool{"Platform": true, "Level": true, "Release": true, "Tags": true, "Exception": true, "EventID": true, "Timestamp": true}
	event := reflect.ValueOf(*got)
	for i := 0; i < event.NumField(); i++ {
		field := event.Type().Field(i)
		if field.IsExported() && !allowed[field.Name] {
			assert.True(t, event.Field(i).IsZero(), "%s must be dropped, got %v", field.Name, event.Field(i))
		}
	}
	assert.Equal(t, polluted.EventID, got.EventID)
	assert.Equal(t, polluted.Timestamp, got.Timestamp)
	assert.Equal(t, sentry.LevelFatal, got.Level)
	assert.Equal(t, reportingtest.Release, got.Release)
	assert.Equal(t, map[string]string{
		"mode": "single", "db": "sqlite", "os": runtime.GOOS + "/" + runtime.GOARCH, "component": "http",
	}, got.Tags, "tags are the process's own, plus the component")
	require.Len(t, got.Exception, 1)
	assert.Equal(t, sentry.Exception{
		Type: "*pq.Error",
		Stacktrace: &sentry.Stacktrace{Frames: []sentry.Frame{{
			Function: "Query", Module: "peekaping/internal/db", Filename: "peekaping/internal/db/db.go", Lineno: 42, InApp: true,
		}}},
	}, got.Exception[0])

	sent, err := json.Marshal(got)
	require.NoError(t, err)
	for _, private := range []string{"admin@example.com", "203.0.113.9", "db-prod-1", "s3cr3t", "acme", "hunter2", "alice", "10.0.0.5", "secret"} {
		assert.NotContains(t, string(sent), private)
	}
}

func TestBeforeSend_DropsAnEventWithoutAnException(t *testing.T) {
	reportingtest.Install(t)

	assert.Nil(t, reporting.BeforeSend(&sentry.Event{Message: "a message is not a panic"}))
}

func TestCapturePanic_SendsNothingWhileOffForcedOffOrWithoutADSN(t *testing.T) {
	// The SDK falls back to SENTRY_DSN when it is given no DSN, which is not a way around the rules
	t.Setenv("SENTRY_DSN", "http://public@127.0.0.1:1/1")
	value, stack := panicked(func() { explode("boom") })
	for name, setup := range map[string]func(*reportingtest.Recorder){
		"off": func(*reportingtest.Recorder) { reporting.SetEnabled(false) },
		"forced off by the environment": func(r *reportingtest.Recorder) {
			reporting.Setup(reporting.Options{DSN: "https://public@sentry.invalid/1", Mode: "single", ForcedOff: true, Transport: r})
			reporting.SetEnabled(true)
		},
		"no DSN": func(r *reportingtest.Recorder) {
			reporting.SetupWithDefaultDSN(reporting.Options{Mode: "single", Transport: r}, "")
			reporting.SetEnabled(true)
		},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := reportingtest.Install(t)
			setup(recorder)
			assert.False(t, reporting.Enabled())

			reporting.CapturePanic("http", value, stack)
			reporting.Guard("job", func() {})
			reporting.Flush(time.Second)

			assert.Empty(t, recorder.Events())
		})
	}
}

// A process that is given no DSN reports to the one built into the binary
func TestSetup_FallsBackToTheBuiltInDSN(t *testing.T) {
	value, stack := panicked(func() { explode("boom") })
	recorder := &reportingtest.Recorder{}
	reporting.SetupWithDefaultDSN(reporting.Options{Mode: "single", Transport: recorder}, "https://public@sentry.invalid/1")
	t.Cleanup(func() { reporting.Setup(reporting.Options{ForcedOff: true}) })
	reporting.SetEnabled(true)

	reporting.CapturePanic("http", value, stack)

	assert.True(t, reporting.Enabled(), "the built-in DSN stands in for an empty ERROR_REPORTING_DSN")
	assert.Len(t, recorder.Events(), 1)
}

func TestEnabled_FollowsSetEnabledUntilTheEnvironmentForcesItOff(t *testing.T) {
	reportingtest.Install(t)
	assert.True(t, reporting.Enabled())
	reporting.SetEnabled(false)
	assert.False(t, reporting.Enabled())
	reporting.SetEnabled(true)
	assert.True(t, reporting.Enabled())

	reporting.Setup(reporting.Options{DSN: "https://public@sentry.invalid/1", ForcedOff: true})
	reporting.SetEnabled(true)
	assert.False(t, reporting.Enabled(), "forced off wins over the admin's choice")
}

func TestSetup_StartsOff(t *testing.T) {
	reportingtest.Install(t)
	require.True(t, reporting.Enabled())

	reporting.Setup(reporting.Options{DSN: "https://public@sentry.invalid/1", Transport: &reportingtest.Recorder{}})

	assert.False(t, reporting.Enabled(), "an admin turns reporting on, not the start of the process")
}

// failingTransport panics when it is given an event
type failingTransport struct{ reportingtest.Recorder }

func (*failingTransport) SendEvent(*sentry.Event) { panic("transport failed") }

func TestCapturePanic_NeverPanicsOnWhatItIsGiven(t *testing.T) {
	reportingtest.Install(t)
	assert.NotPanics(t, func() {
		reporting.CapturePanic("http", nil, nil)
		reporting.CapturePanic("http", "boom", []byte("not a stack at all\n\t\n(((\n\tfile.go:notanumber"))
		reporting.CapturePanic("http", errors.New("boom"), bytes.Repeat([]byte("pkg.F(...)\n\t/x/y.go:1 +0x1\n"), 10000))
	})

	reporting.Setup(reporting.Options{DSN: "https://public@sentry.invalid/1", Transport: &failingTransport{}})
	reporting.SetEnabled(true)
	assert.NotPanics(t, func() { reporting.CapturePanic("http", "boom", nil) }, "a transport that fails")
}

func TestCapturePanic_KeepsTheInnermostHundredFrames(t *testing.T) {
	recorder := reportingtest.Install(t)
	var stack strings.Builder
	stack.WriteString("goroutine 1 [running]:\nruntime/debug.Stack()\n\t/go/src/runtime/debug/stack.go:26 +0x5e\npanic({0x1, 0x2})\n\t/go/src/runtime/panic.go:1 +0x1\n")
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&stack, "peekaping/internal/x.F%d(0x1)\n\t/src/x.go:%d +0x1\n", i, i+1)
	}

	reporting.CapturePanic("http", "boom", []byte(stack.String()))

	frames := recorder.Events()[0].Exception[0].Stacktrace.Frames
	require.Len(t, frames, 100)
	assert.Equal(t, "F0", frames[len(frames)-1].Function, "the frame that panicked is kept")
	assert.Equal(t, "F99", frames[0].Function)
}

func TestCapturePanic_ReportsAPanicSiteOnceAMinute(t *testing.T) {
	recorder := reportingtest.Install(t)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	defer reporting.SetNow(func() time.Time { return now })()
	value, stack := panicked(func() { explode("boom") })
	otherValue, otherStack := panicked(func() { outOfRange(9) })

	for range 50 {
		reporting.CapturePanic("scheduler", value, stack)
	}
	reporting.CapturePanic("scheduler", otherValue, otherStack)
	assert.Len(t, recorder.Events(), 2, "a check that panics on every run is one report, another place is another")

	reporting.CapturePanic("events", value, stack)
	assert.Len(t, recorder.Events(), 3, "the same panic recovered somewhere else is another place")

	now = now.Add(time.Minute + time.Second)
	reporting.CapturePanic("scheduler", value, stack)
	assert.Len(t, recorder.Events(), 4, "and a minute later it is reported again")
}

func TestGuard_ReportsAFatalEventFlushesAndPanicsAgainWithTheSameValue(t *testing.T) {
	recorder := reportingtest.Install(t)
	original := errors.New("boom")

	value, _ := panicked(func() {
		reporting.Guard("watchdog", func() { explode(original) })
	})

	assert.Same(t, original, value, "the crash is the one the code had without the guard")
	sent := recorder.Sent()
	require.Len(t, sent, 1, "the report is flushed before the panic goes on")
	assert.Equal(t, sentry.LevelFatal, sent[0].Level)
	assert.Equal(t, "watchdog", sent[0].Tags["component"])
	assert.Equal(t, "*errors.errorString", sent[0].Exception[0].Type)
	assert.Contains(t, functions(sent[0].Exception[0].Stacktrace.Frames), "explode")
}

func TestReportCrash_ReportsAPanicOfTheCallerAndPanicsAgain(t *testing.T) {
	recorder := reportingtest.Install(t)
	original := errors.New("boom")

	value, _ := panicked(func() {
		defer reporting.ReportCrash("main")
		explode(original)
	})

	assert.Same(t, original, value)
	sent := recorder.Sent()
	require.Len(t, sent, 1, "flushed before the panic goes on")
	assert.Equal(t, sentry.LevelFatal, sent[0].Level)
	assert.Equal(t, "main", sent[0].Tags["component"])
	assert.Contains(t, functions(sent[0].Exception[0].Stacktrace.Frames), "explode")
}

func TestReportCrash_LeavesAReturnAloneAndAPanicWhileReportingIsOff(t *testing.T) {
	recorder := reportingtest.Install(t)

	func() { defer reporting.ReportCrash("main") }()
	reporting.SetEnabled(false)
	value, _ := panicked(func() {
		defer reporting.ReportCrash("main")
		explode("boom")
	})

	assert.Equal(t, "boom", value)
	assert.Empty(t, recorder.Events())
}

func TestGuard_LeavesAReturningFunctionAlone(t *testing.T) {
	recorder := reportingtest.Install(t)
	ran := false

	reporting.Guard("watchdog", func() { ran = true })

	assert.True(t, ran)
	assert.Empty(t, recorder.Events())
}

func TestGuard_LeavesAPanicAloneWhileReportingIsOff(t *testing.T) {
	recorder := reportingtest.Install(t)
	reporting.SetEnabled(false)

	value, _ := panicked(func() {
		reporting.Guard("watchdog", func() { explode("boom") })
	})

	assert.Equal(t, "boom", value)
	assert.Empty(t, recorder.Events())
}

func TestGo_RunsFnInAnotherGoroutine(t *testing.T) {
	reportingtest.Install(t)
	done := make(chan struct{})

	reporting.Go("metrics", func() { close(done) })

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("fn did not run")
	}
}

// flushingTransport queues events and prints them when it is flushed, so that the output shows
// what had been flushed when the process died
type flushingTransport struct {
	mu     sync.Mutex
	queued []*sentry.Event
}

func (*flushingTransport) Configure(sentry.ClientOptions) {}
func (*flushingTransport) Close()                         {}
func (s *flushingTransport) SendEvent(e *sentry.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queued = append(s.queued, e)
}
func (s *flushingTransport) Flush(time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.queued {
		fmt.Printf("SENT level=%s type=%s component=%s\n", e.Level, e.Exception[0].Type, e.Tags["component"])
	}
	s.queued = nil
	return true
}
func (s *flushingTransport) FlushWithContext(context.Context) bool { return s.Flush(0) }

// TestGoCrashHelper is the process TestGo_CrashesTheProcessAfterTheReportIsFlushed starts
func TestGoCrashHelper(t *testing.T) {
	if os.Getenv("REPORTING_CRASH_HELPER") != "1" {
		t.Skip("started by TestGo_CrashesTheProcessAfterTheReportIsFlushed")
	}
	reporting.Setup(reporting.Options{DSN: "https://public@sentry.invalid/1", Mode: "single", Transport: &flushingTransport{}})
	reporting.SetEnabled(true)
	reporting.Go("producer", func() { explode("boom") })
	time.Sleep(time.Minute)
}

func TestGo_CrashesTheProcessAfterTheReportIsFlushed(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestGoCrashHelper$", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), "REPORTING_CRASH_HELPER=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()

	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit, "the process crashed")
	assert.Equal(t, 2, exit.ExitCode(), "with the exit status of a panic")
	assert.Contains(t, stderr.String(), "panic: boom", "and the panic as it was")
	assert.Contains(t, stdout.String(), "SENT level=fatal type=string component=producer", "after the report was flushed")
}

// The event the SDK is given is not what goes on the wire: the transport adds an envelope header,
// and the SDK may add more later. This reads what a server at the DSN really receives.
//
// It is the one test that goes through the transport production uses. The other tests hand the
// SDK a fake sentry.Transport, and sentry-go v0.50 then skips its telemetry buffer (client.go,
// "Cannot enable Telemetry Processor with custom Transport"), which is the path real events take.
// So no Transport is set here: the SDK builds its own and sends to the httptest server.
func TestSetup_WhatReachesTheServerHoldsOnlyTheAllowlist(t *testing.T) {
	type request struct {
		path    string
		headers http.Header
		body    []byte
	}
	received := make(chan request, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- request{path: r.URL.Path, headers: r.Header.Clone(), body: body}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")

	reporting.Setup(reporting.Options{DSN: "http://public@" + host + "/1", Release: "1.2.3", Mode: "cluster", DB: "postgres"})
	reporting.SetEnabled(true)
	defer reporting.Setup(reporting.Options{ForcedOff: true})
	value, stack := panicked(func() { explode(errorHoldingSecrets()) })

	reporting.CapturePanic("http", value, stack)
	reporting.Flush(5 * time.Second)

	var got request
	select {
	case got = <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("the server received nothing")
	}
	assert.Equal(t, "/api/1/envelope/", got.path)

	// The raw bytes hold none of what the error's text held, nor this machine's name...
	for _, secret := range secrets {
		assert.NotContains(t, string(got.body), secret)
	}
	hostname, _ := os.Hostname()
	if len(hostname) >= 4 {
		assert.NotContains(t, string(got.body), hostname)
	}
	// ...and do hold what a report is for: the type, a frame, the release and the tags
	for _, held := range []string{
		`"type":"*errors.errorString"`,
		`"function":"explode"`,
		`"release":"1.2.3"`,
		`"mode":"cluster"`,
		`"db":"postgres"`,
		`"component":"http"`,
	} {
		assert.Contains(t, string(got.body), held)
	}

	// An envelope is one JSON object per line: its header, then each item's header and payload
	lines := strings.Split(strings.TrimSpace(string(got.body)), "\n")
	require.Len(t, lines, 3, string(got.body))
	var envelopeHeader, itemHeader, event map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &envelopeHeader))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &itemHeader))
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &event))
	assert.Equal(t, "event", itemHeader["type"])
	assert.Subset(t, []string{"event_id", "sent_at", "sdk", "dsn"}, keys(envelopeHeader), "envelope header")
	assert.Subset(t, []string{"event_id", "timestamp", "platform", "level", "release", "tags", "exception", "sdk"}, keys(event), "event")
	assert.Empty(t, event["sdk"], "the SDK adds an empty sdk object, which names nothing")
	assert.Empty(t, envelopeHeader["sdk"])
	assert.True(t, strings.HasSuffix(event["timestamp"].(string), "Z"), "the time is UTC, so it doesn't give the server's time zone: %v", event["timestamp"])
	assert.Equal(t, "1.2.3", event["release"])
	assert.Equal(t, "error", event["level"])
	exception := event["exception"].([]any)[0].(map[string]any)
	assert.Subset(t, []string{"type", "stacktrace"}, keys(exception), "exception")
	frame := exception["stacktrace"].(map[string]any)["frames"].([]any)[0].(map[string]any)
	assert.Subset(t, []string{"function", "module", "filename", "lineno", "in_app"}, keys(frame), "frame")

	for _, header := range []string{"Cookie", "Authorization", "X-Forwarded-For"} {
		assert.Empty(t, got.headers.Get(header), header)
	}
}

func keys(m map[string]any) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	return names
}
