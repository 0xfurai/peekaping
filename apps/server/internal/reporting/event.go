package reporting

import (
	"path"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
)

// componentTag is the tag that names where a panic was recovered
const componentTag = "component"

// maxFrames is how many frames a report holds: the innermost ones, where the panic is
const maxFrames = 100

// maxNameLen bounds every string taken from a frame
const maxNameLen = 300

// tagValue is what a tag may hold: names from the code and the build, such as "cluster" or
// "linux/arm64". Anything else is not sent.
var tagValue = regexp.MustCompile(`^[A-Za-z0-9_./-]{1,32}$`)

func safeTag(value string) string {
	if tagValue.MatchString(value) {
		return value
	}
	return "unknown"
}

// beforeSend is the SDK's last hook before an event goes out. It does not clean the event it
// is given: it builds a new one from the allowlist in ADR 0003 section 4, which is the error
// type, the stack frames (function, package, file, line), the release, and the tags mode, db,
// os and component. The event ID and time are what the protocol needs to take the event at
// all. Whatever else the SDK or our code put on the event is not copied.
func (st *state) beforeSend(in *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if in == nil || len(in.Exception) == 0 {
		return nil
	}
	source := in.Exception[0]
	level := sentry.LevelError
	if in.Level == sentry.LevelFatal {
		level = sentry.LevelFatal
	}

	var frames []sentry.Frame
	if source.Stacktrace != nil {
		from := source.Stacktrace.Frames
		if len(from) > maxFrames {
			from = from[len(from)-maxFrames:]
		}
		frames = make([]sentry.Frame, 0, len(from))
		for _, f := range from {
			frames = append(frames, sentry.Frame{
				Function: capped(f.Function),
				Module:   capped(f.Module),
				Filename: relativeFile(f.Filename),
				Lineno:   f.Lineno,
				InApp:    f.InApp,
			})
		}
	}

	return &sentry.Event{
		EventID:   in.EventID,
		Timestamp: in.Timestamp.UTC(), // in UTC, not in the server's time zone
		Platform:  "go",
		Level:     level,
		Release:   st.release,
		Tags: map[string]string{
			"mode":       safeTag(st.mode),
			"db":         safeTag(st.db),
			"os":         runtime.GOOS + "/" + runtime.GOARCH,
			componentTag: safeTag(in.Tags[componentTag]),
		},
		Exception: []sentry.Exception{{
			Type:       capped(source.Type),
			Stacktrace: &sentry.Stacktrace{Frames: frames},
		}},
	}
}

func capped(s string) string {
	if len(s) > maxNameLen {
		return s[:maxNameLen]
	}
	return s
}

// relativeFile keeps a file name from naming the machine that built the binary: a path that
// is absolute is cut down to its last element.
func relativeFile(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	if path.IsAbs(name) || strings.Contains(name, ":") {
		name = path.Base(name)
	}
	return capped(name)
}

// parseStack turns debug.Stack() into frames, the oldest first as Sentry wants them, and
// the innermost maxFrames of them. The text looks like
//
//	goroutine 7 [running]:
//	runtime/debug.Stack()
//		/usr/local/go/src/runtime/debug/stack.go:26 +0x5e
//	peekaping/internal/httpx.Recovery.func1.1()
//		/src/apps/server/internal/httpx/middleware.go:14 +0x4f
//	panic({0x1048a0, 0x1048b8})
//		/usr/local/go/src/runtime/panic.go:860 +0x13c
//	peekaping/internal/monitor.(*Service).Check(0x14000, {0x10, 0x20})
//		/src/apps/server/internal/monitor/service.go:88 +0x1c
//
// Only the function names and the lines are read. The arguments are addresses, and the path
// of a file is cut to its package and name: a source path names the machine that built the
// binary, which is not ours to send.
func parseStack(stack []byte) []sentry.Frame {
	frames := withoutHookFrames(printedFrames(stack))
	if len(frames) > maxFrames {
		frames = frames[:maxFrames]
	}
	for i, j := 0, len(frames)-1; i < j; i, j = i+1, j-1 {
		frames[i], frames[j] = frames[j], frames[i]
	}
	return frames
}

// printedFrames reads the frames of the stack text, innermost first as they are printed
func printedFrames(stack []byte) []sentry.Frame {
	var frames []sentry.Frame
	var pending *sentry.Frame
	for _, text := range strings.Split(string(stack), "\n") {
		if text == "" {
			continue
		}
		if text[0] != '\t' && text[0] != ' ' {
			if pending != nil {
				frames = append(frames, *pending)
			}
			pending = functionFrame(text)
			continue
		}
		addFile(pending, text)
	}
	if pending != nil {
		frames = append(frames, *pending)
	}
	return frames
}

// functionFrame is the frame a function line starts, or nil when the line is not one
func functionFrame(text string) *sentry.Frame {
	f, ok := parseFunctionLine(text)
	if !ok {
		return nil
	}
	return &f
}

// addFile gives the frame the file and line of a file line, unless it has a line already
func addFile(pending *sentry.Frame, text string) {
	if pending != nil && pending.Lineno == 0 {
		if file, n, ok := parseFileLine(text); ok {
			pending.Lineno = n
			pending.Filename = frameFile(pending.Module, file)
		}
	}
}

// parseFunctionLine reads "pkg/path.(*Type).Method(0x1, {0x2, 0x3})". The package path ends at
// the first dot after the last slash.
func parseFunctionLine(text string) (sentry.Frame, bool) {
	if !strings.HasSuffix(text, ")") {
		return sentry.Frame{}, false
	}
	open := strings.LastIndex(text, "(")
	if open <= 0 {
		return sentry.Frame{}, false
	}
	name := text[:open]
	module, function := "", name
	slash := strings.LastIndex(name, "/")
	if dot := strings.Index(name[slash+1:], "."); dot >= 0 {
		module, function = name[:slash+1+dot], name[slash+1+dot+1:]
	}
	return sentry.Frame{Function: capped(function), Module: capped(module), InApp: inApp(module)}, true
}

// parseFileLine reads "\t/path/to/file.go:42 +0x5e"
func parseFileLine(text string) (file string, line int, ok bool) {
	text = strings.TrimSpace(text)
	if i := strings.LastIndex(text, " +0x"); i >= 0 {
		text = text[:i]
	}
	colon := strings.LastIndex(text, ":")
	if colon <= 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(text[colon+1:])
	if err != nil || n <= 0 {
		return "", 0, false
	}
	return text[:colon], n, true
}

// frameFile is the package's import path and the file's name: "peekaping/internal/httpx/middleware.go"
func frameFile(module, file string) string {
	base := path.Base(strings.ReplaceAll(file, `\`, "/"))
	if module == "" {
		return base
	}
	return capped(module + "/" + base)
}

// inApp is true for Peekaping's own code: the module "peekaping", and package main, which is
// all that a binary's main package prints.
func inApp(module string) bool {
	return module == "main" || module == "peekaping" || strings.HasPrefix(module, "peekaping/")
}

// withoutHookFrames drops the innermost frames, which belong to the hook that took the stack
// and not to the panic: runtime/debug.Stack, the deferred function that called it, the runtime's
// panic and the runtime functions that raised it (goPanicIndex, sigpanic). The frames that are
// left start at the code that panicked.
func withoutHookFrames(frames []sentry.Frame) []sentry.Frame {
	for i, f := range frames {
		if f.Module == "" && f.Function == "panic" {
			rest := i + 1
			for rest < len(frames) && frames[rest].Module == "runtime" {
				rest++
			}
			if rest < len(frames) {
				return frames[rest:]
			}
			break
		}
	}
	// A stack taken outside a panic: cut what is the hook's
	rest := 0
	for rest < len(frames) && (frames[rest].Module == "runtime/debug" || frames[rest].Module == "peekaping/internal/reporting") {
		rest++
	}
	return frames[rest:]
}

// siteLimiter lets one place report a panic once a minute. A check that panics on every run
// would otherwise send a report each time: a monitor every few seconds, for every monitor.
// A place is the component, the type and the innermost frame.
type siteLimiter struct {
	mu   sync.Mutex
	last map[string]time.Time
}

const (
	siteWindow = time.Minute
	// maxSites bounds the memory: past it, new places wait for old ones to expire
	maxSites = 1000
)

func newSiteLimiter() *siteLimiter { return &siteLimiter{last: make(map[string]time.Time)} }

func (l *siteLimiter) allow(component string, exception sentry.Exception, now time.Time) bool {
	key := component + "|" + exception.Type
	if frames := exception.Stacktrace.Frames; len(frames) > 0 {
		top := frames[len(frames)-1]
		key += "|" + top.Module + "." + top.Function + ":" + strconv.Itoa(top.Lineno)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if at, seen := l.last[key]; seen && now.Sub(at) < siteWindow {
		return false
	}
	if len(l.last) >= maxSites {
		for k, at := range l.last {
			if now.Sub(at) >= siteWindow {
				delete(l.last, k)
			}
		}
		if len(l.last) >= maxSites {
			return false
		}
	}
	l.last[key] = now
	return true
}
