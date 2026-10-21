// Package reportingtest sets up error reporting for tests with a transport that keeps the
// events instead of sending them.
package reportingtest

import (
	"context"
	"sync"
	"testing"
	"time"

	"peekaping/internal/reporting"

	"github.com/getsentry/sentry-go"
)

// Recorder is a sentry.Transport that keeps the events it is given. Like the real transport it
// sends in the background: an event is queued by SendEvent and counts as sent once Flush ran.
type Recorder struct {
	mu     sync.Mutex
	queued []*sentry.Event
	sent   []*sentry.Event
}

func (r *Recorder) Configure(sentry.ClientOptions) {}

func (r *Recorder) SendEvent(event *sentry.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queued = append(r.queued, event)
}

func (r *Recorder) Flush(time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, r.queued...)
	r.queued = nil
	return true
}

func (r *Recorder) FlushWithContext(context.Context) bool { return r.Flush(0) }

func (r *Recorder) Close() {}

// Events are the events handed to the transport, sent or still queued.
func (r *Recorder) Events() []*sentry.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append(append([]*sentry.Event(nil), r.sent...), r.queued...)
}

// Sent are the events that a Flush has sent.
func (r *Recorder) Sent() []*sentry.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*sentry.Event(nil), r.sent...)
}

// What Install sets reporting up with
const (
	Release = "9.8.7"
	Mode    = "single"
	DB      = "sqlite"
)

// Install sets up reporting with a Recorder and turns it on. Turn it off again with
// reporting.SetEnabled(false). Reporting is reset when the test ends.
func Install(t testing.TB) *Recorder {
	t.Helper()
	recorder := &Recorder{}
	reporting.Setup(reporting.Options{
		DSN:       "https://public@sentry.invalid/1",
		Release:   Release,
		Mode:      Mode,
		DB:        DB,
		Transport: recorder,
	})
	reporting.SetEnabled(true)
	t.Cleanup(func() { reporting.Setup(reporting.Options{}) })
	return recorder
}
