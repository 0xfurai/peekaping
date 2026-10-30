package mailer_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"testing"

	"peekaping/internal/mailer"

	"github.com/stretchr/testify/assert"
)

// timeoutErr is a net.Error that is not an OS error: only its Timeout method says it timed out
type timeoutErr struct{ timeout bool }

func (e timeoutErr) Error() string   { return "i/o with 192.0.2.10 took too long" }
func (e timeoutErr) Timeout() bool   { return e.timeout }
func (e timeoutErr) Temporary() bool { return false }

func TestDescribe_ReadsTheRestOfTheFailureClassesInOrder(t *testing.T) {
	server := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 587}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"a cancelled send", fmt.Errorf("failed to send email: %w", context.Canceled), "send failed: cancelled"},
		{"an operation that failed with a plain error", &net.OpError{Op: "dial", Net: "tcp", Addr: server, Err: errors.New("dial 192.0.2.10 refused")}, "send failed: dial: *errors.errorString"},
		{"an operation that failed with a net error that timed out", &net.OpError{Op: "write", Net: "tcp", Addr: server, Err: timeoutErr{timeout: true}}, "send failed: write: timed out"},
		{"an operation without a name", &net.OpError{Net: "tcp", Addr: server, Err: errors.New("broken")}, "send failed: *errors.errorString"},
		{"a net error that timed out", fmt.Errorf("smtp: %w", timeoutErr{timeout: true}), "send failed: timed out"},
		{"a net error that did not time out", fmt.Errorf("smtp: %w", timeoutErr{}), "send failed: mailer_test.timeoutErr"},
		{"a name that is not found and a timeout", &net.DNSError{Err: "x", Name: "smtp.example.com", IsNotFound: true, IsTimeout: true}, "send failed: the host name could not be resolved (not found)"},
		{"a reply beside a lookup failure", fmt.Errorf("%w: %w", &textproto.Error{Code: 421}, &net.DNSError{Err: "x", IsNotFound: true}), "the server replied 421"},
		{"an expired send beside a cancelled one", fmt.Errorf("%w: %w", context.DeadlineExceeded, context.Canceled), "send failed: timed out"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, mailer.Describe(tc.err))
		})
	}
}
