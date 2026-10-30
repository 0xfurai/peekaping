package probeclient

import (
	"context"
	"time"
)

// Failed is failed, for the tests of package probeclient_test
func (s *Session) Failed(call string, err error, backoff *Backoff) time.Duration {
	return s.failed(call, err, backoff)
}

// Greeted is whether the session still holds its hello
func (s *Session) Greeted() bool { return s.isGreeted() }

// GuardRedirects is guardRedirects, for the tests of package probeclient_test
var GuardRedirects = guardRedirects

// SayHelloAgain makes the probe forget its hello and say it again, as it does after a lost
// connection
func (p *Probe) SayHelloAgain(ctx context.Context) error {
	p.session.lose()
	return p.session.Connect(ctx)
}
