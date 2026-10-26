// Package reportingtest sets up error reporting for tests with a transport that keeps the
// events instead of sending them.
package reportingtest

import (
	"context"
	"sync"
	"testing"
	"time"

	"peekaping/internal/reporting"

	"github.com/alicebob/miniredis/v2"
	"github.com/getsentry/sentry-go"
	"github.com/hibiken/asynq"
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

// Wait returns the events once there are n of them, or fails the test after a few seconds.
// For a panic that is reported from another goroutine.
func (r *Recorder) Wait(t testing.TB, n int) []*sentry.Event {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if events := r.Events(); len(events) >= n {
			return events
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d reported events, got %d", n, len(r.Events()))
	return nil
}

// NewAsynq returns an asynq server and client on an in-process Redis, for a test that runs a
// task through the code that serves it. The server takes tasks from the default queue; a test
// starts it with the mux of the code under test and stops it with server.Shutdown.
func NewAsynq(t testing.TB) (*asynq.Server, *asynq.Client) {
	t.Helper()
	redis := miniredis.RunT(t)
	opt := asynq.RedisClientOpt{Addr: redis.Addr()}
	server := asynq.NewServer(opt, asynq.Config{Concurrency: 1, Queues: map[string]int{"default": 1}})
	client := asynq.NewClient(opt)
	t.Cleanup(func() { _ = client.Close() })
	return server, client
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
	// ForcedOff builds no client, so that no later test can reach the built-in DSN
	t.Cleanup(func() { reporting.Setup(reporting.Options{ForcedOff: true}) })
	return recorder
}
