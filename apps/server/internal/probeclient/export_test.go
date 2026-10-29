package probeclient

import "context"

// GuardRedirects is guardRedirects, for the tests of package probeclient_test
var GuardRedirects = guardRedirects

// SayHelloAgain makes the probe forget its hello and say it again, as it does after a lost
// connection
func (p *Probe) SayHelloAgain(ctx context.Context) error {
	p.session.lose()
	return p.session.Connect(ctx)
}
