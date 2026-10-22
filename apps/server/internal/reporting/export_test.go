package reporting

import (
	"time"

	"github.com/getsentry/sentry-go"
)

// SetupWithDefaultDSN is Setup with another built-in DSN, so that a test can try the empty one
// whatever DefaultDSN holds
func SetupWithDefaultDSN(opts Options, defaultDSN string) { setup(opts, defaultDSN) }

// BeforeSend is the hook the Sentry client calls with the event it is about to send
func BeforeSend(event *sentry.Event) *sentry.Event { return current.Load().beforeSend(event, nil) }

// SetNow changes the clock the once-a-minute rule reads, and returns how to put it back
func SetNow(now func() time.Time) (restore func()) {
	saved := clock
	clock = now
	return func() { clock = saved }
}
