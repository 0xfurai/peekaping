// Package reporting is the error report (ticket #233, ADR 0003): a crash report sent to Sentry
// until an admin opts out. It holds the state of the process (whether reporting is on) as package
// state, because the places that recover a panic live in many packages and none of them should
// need a dependency to report it.
//
// A report holds the error type, the stack frames, the release and four tags, and nothing else.
// The event the SDK builds is never sent as it is: BeforeSend builds a new one from that list
// (see event.go), so a field the SDK adds later is not sent either.
package reporting

import (
	"fmt"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go"
)

// DefaultDSN is Peekaping's Sentry project, where error reports go unless ERROR_REPORTING_DSN names
// another. A DSN is not a secret: it only lets a client send events to the project. An empty DSN
// sends nothing. A test must give Setup a DSN of its own, a Transport or ForcedOff, so that none
// can reach this project (see builtin_dsn_guard_test.go).
const DefaultDSN = "https://21a7d557a242d6e6ec3f8551841ea2db@o90647.ingest.us.sentry.io/4512301050691584"

// flushTimeout is how long a crash waits for its report to go out before the process dies
const flushTimeout = 2 * time.Second

// Options set up error reporting for one process.
type Options struct {
	// DSN overrides DefaultDSN (ERROR_REPORTING_DSN)
	DSN string
	// Release is the Peekaping version
	Release string
	// Mode is how the install runs: "single" or "cluster"
	Mode string
	// DB is the storage backend: "sqlite", "postgres", "mysql", or "unknown"
	DB string
	// ForcedOff is true when ERROR_REPORTING switches reporting off. Nothing is
	// sent then, whatever SetEnabled is given.
	ForcedOff bool
	// Transport replaces the HTTP transport. Tests use it to catch the events.
	Transport sentry.Transport
}

// state is what Setup built. It is replaced as a whole, so a reader never sees half of it.
type state struct {
	// client is nil when nothing can be sent: no DSN, or forced off
	client  *sentry.Client
	release string
	mode    string
	db      string
	limiter *siteLimiter
}

var (
	current atomic.Pointer[state]
	enabled atomic.Bool
	// clock is the time the once-a-minute rule reads; a test changes it
	clock = time.Now
)

// Setup prepares the process to report. Reporting stays off until SetEnabled(true).
func Setup(opts Options) { setup(opts, DefaultDSN) }

func setup(opts Options, defaultDSN string) {
	enabled.Store(false)
	previous := current.Swap(nil)
	if previous != nil && previous.client != nil {
		previous.client.Close()
	}

	dsn := opts.DSN
	if dsn == "" {
		dsn = defaultDSN
	}
	next := &state{release: opts.Release, mode: opts.Mode, db: opts.DB, limiter: newSiteLimiter()}
	// The SDK falls back to SENTRY_DSN when the DSN is empty, so it is not even asked then
	if dsn != "" && !opts.ForcedOff {
		client, err := sentry.NewClient(sentry.ClientOptions{
			Dsn:     dsn,
			Release: opts.Release,
			// Off, so that nothing user-related is added by the SDK's own integrations
			SendDefaultPII: false,
			// None of the default integrations: they add the OS, the runtime, the module list
			// and the environment, which are not in the allowlist
			Integrations:         func([]sentry.Integration) []sentry.Integration { return nil },
			BeforeSend:           next.beforeSend,
			BeforeBreadcrumb:     func(*sentry.Breadcrumb, *sentry.BreadcrumbHint) *sentry.Breadcrumb { return nil },
			MaxBreadcrumbs:       -1,
			DisableClientReports: true,
			Transport:            opts.Transport,
		})
		if err == nil {
			next.client = client
		}
	}
	current.Store(next)
}

// SetEnabled turns reporting on or off. It has no effect while the environment forces it off.
func SetEnabled(on bool) { enabled.Store(on) }

// Enabled reports whether a panic would be sent now.
func Enabled() bool {
	st := current.Load()
	return enabled.Load() && st != nil && st.client != nil
}

// CapturePanic reports a recovered panic: component names where it was recovered, value is what
// recover() returned, and stack is debug.Stack() taken while the panic was in flight. It sends
// nothing while reporting is off, never panics, and never waits for the network.
func CapturePanic(component string, value any, stack []byte) {
	capture(component, value, stack, sentry.LevelError)
}

func capture(component string, value any, stack []byte, level sentry.Level) {
	defer func() { _ = recover() }() // a failing report must not take down the code that reports
	st := current.Load()
	if !Enabled() {
		return
	}
	exception := sentry.Exception{Type: typeName(value), Stacktrace: &sentry.Stacktrace{Frames: parseStack(stack)}}
	// A crash is reported whatever came before: the process is about to end, so it can't repeat
	if level != sentry.LevelFatal && !st.limiter.allow(component, exception, clock()) {
		return
	}
	st.client.CaptureEvent(&sentry.Event{
		Level:     level,
		Tags:      map[string]string{componentTag: component},
		Exception: []sentry.Exception{exception},
	}, nil, nil)
}

// Go runs fn in a new goroutine. A panic in fn is reported and flushed, then raised again, so
// the process crashes as it would have without Go.
func Go(component string, fn func()) { go Guard(component, fn) }

// Guard is Go on the calling goroutine. While reporting is off it does not recover the panic,
// so a crash looks as it would without the guard.
func Guard(component string, fn func()) {
	returned := false
	defer func() {
		if returned || !Enabled() {
			return
		}
		value := recover()
		if value == nil {
			return // runtime.Goexit
		}
		capture(component, value, debug.Stack(), sentry.LevelFatal)
		Flush(flushTimeout)
		panic(value)
	}()
	fn()
	returned = true
}

// ReportCrash is deferred at the top of a function whose panic ends the process, such as main:
//
//	defer reporting.ReportCrash("main")
//
// A panic that reaches it is reported as fatal and flushed, then raised again with the same
// value, so the process crashes as it would have. It must be deferred directly: recover only
// works in the deferred function itself.
func ReportCrash(component string) {
	if !Enabled() {
		return
	}
	value := recover()
	if value == nil {
		return
	}
	capture(component, value, debug.Stack(), sentry.LevelFatal)
	Flush(flushTimeout)
	panic(value)
}

// Flush waits up to timeout for the reports queued to be sent.
func Flush(timeout time.Duration) {
	defer func() { _ = recover() }()
	if st := current.Load(); st != nil && st.client != nil {
		st.client.Flush(timeout)
	}
}

// typeName is the Go type of what was panicked with: runtime.boundsError, string,
// *errors.errorString. It is a name from the code, never from the data.
func typeName(value any) string {
	if value == nil {
		return "nil"
	}
	name := fmt.Sprintf("%T", value)
	if len(name) > maxTypeLen {
		name = name[:maxTypeLen]
	}
	return name
}

const maxTypeLen = 200
