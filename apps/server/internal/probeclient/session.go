package probeclient

import (
	"context"
	"errors"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"peekaping/internal/probeproto"
)

const (
	// upgradeRetry is how long the probe waits to say hello again to a server that needs a newer
	// protocol version than the probe speaks. An upgrade of the probe is what it waits for.
	upgradeRetry = time.Hour
	// maxClockSkew is how far the server's clock may be from the probe's before the probe warns
	maxClockSkew = 30 * time.Second
	// requestTimeout bounds every call but the long poll, so that a connection that went silent
	// doesn't hold the probe for good
	requestTimeout = 30 * time.Second
	// pollSlack is how long past the server's poll timeout the probe waits for the poll's answer
	pollSlack = 30 * time.Second
	// maxRetryAfter is the longest the probe waits on a Retry-After, whatever the server asks, so
	// that a server gone wrong can't silence the probe for hours
	maxRetryAfter = 5 * time.Minute
	// callHello names the call that says hello, as failed is told of it
	callHello = "say hello"
)

// The limits the probe works within until the server's hello states them: the server's own defaults
const (
	defaultPollTimeout   = 25 * time.Second
	defaultFlushInterval = 2 * time.Second
	defaultBatchMax      = 500
	defaultBufferMax     = 10000
)

// SessionConfig is what a Session needs
type SessionConfig struct {
	// URL is the server's origin, such as https://peekaping.example.com
	URL string
	// Token is the probe's token
	Token string
	// ProbeVersion is this probe's version, which hello states
	ProbeVersion string
	// SupportedTypes are the monitor types the probe runs, which hello states
	SupportedTypes []string
	// HTTPClient makes the requests; http.DefaultClient when nil. The session bounds each call
	// itself, so the client needs no timeout, and one it has must outlast the long poll. The
	// session uses a copy of it that refuses a redirect from https to http; the client itself is
	// not changed.
	HTTPClient *http.Client
	Logger     *zap.SugaredLogger

	// Sleep waits for d, or returns the context's error when it ends first. It is a seam for
	// tests, and the real wait when nil.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now is the probe's clock; time.Now when nil
	Now func() time.Time
}

// Tunables are the limits the server's hello states, or their defaults before there is one
type Tunables struct {
	// PollTimeout is how long the server holds a poll before it answers 304
	PollTimeout time.Duration
	// FlushInterval is how long after its first result the probe sends a batch
	FlushInterval time.Duration
	// BatchMax is the most results in one batch
	BatchMax int
	// BufferMax is the most results the probe holds while it can't reach the server
	BufferMax int
}

// Session is the probe's standing with the server: the hello it says to join, and the rules for
// what to do when a call to the server fails. The source's poll and the sink's flusher share one
// session, so that a hello said after a lost connection serves both.
type Session struct {
	client *probeproto.Client
	hello  probeproto.HelloRequest
	base   *zap.SugaredLogger
	logger *zap.SugaredLogger
	sleep  func(ctx context.Context, d time.Duration) error
	now    func() time.Time

	// gate lets one caller say hello at a time. It is a channel so that a caller waiting for
	// its turn can give up when its context ends.
	gate chan struct{}

	mu      sync.Mutex
	welcome *probeproto.HelloResponse
	// greeted is set when hello was answered and no call has failed since
	greeted bool
}

// NewSession creates a session with a random instance id. It makes no call: Connect says hello.
func NewSession(cfg SessionConfig) *Session {
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop().Sugar()
	}
	hostname, _ := os.Hostname()
	instanceID := uuid.NewString()
	s := &Session{
		client: probeproto.NewClient(cfg.URL, cfg.Token, instanceID, guardRedirects(cfg.HTTPClient)),
		hello: probeproto.HelloRequest{
			InstanceID:      instanceID,
			ProtocolVersion: probeproto.ProtocolVersion,
			ProbeVersion:    cfg.ProbeVersion,
			SupportedTypes:  cfg.SupportedTypes,
			Hostname:        hostname,
			OS:              runtime.GOOS,
			Arch:            runtime.GOARCH,
		},
		base:   cfg.Logger,
		logger: cfg.Logger.With("component", "probe_session"),
		sleep:  cfg.Sleep,
		now:    cfg.Now,
		gate:   make(chan struct{}, 1),
	}
	if s.sleep == nil {
		s.sleep = sleepFor
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// wait waits for d, or returns the context's error when it ends first. A wait of nothing is no wait.
func (s *Session) wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	return s.sleep(ctx, d)
}

// sleepFor waits for d, or returns the context's error when it ends first
func sleepFor(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// loggerOf is the session's logger for a component of the probe client
func (s *Session) loggerOf(component string) *zap.SugaredLogger {
	return s.base.With("component", component)
}

// InstanceID is the random id of this process, which every request carries
func (s *Session) InstanceID() string { return s.hello.InstanceID }

// Welcome is the server's latest answer to hello, or nil before there is one
func (s *Session) Welcome() *probeproto.HelloResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.welcome == nil {
		return nil
	}
	welcome := *s.welcome
	return &welcome
}

// Tunables are the limits the latest hello stated. A limit it left out, or before any hello, is the
// server's default.
func (s *Session) Tunables() Tunables {
	tunables := Tunables{
		PollTimeout:   defaultPollTimeout,
		FlushInterval: defaultFlushInterval,
		BatchMax:      defaultBatchMax,
		BufferMax:     defaultBufferMax,
	}
	welcome := s.Welcome()
	if welcome == nil {
		return tunables
	}
	if welcome.PollTimeoutMs > 0 {
		tunables.PollTimeout = time.Duration(welcome.PollTimeoutMs) * time.Millisecond
	}
	if welcome.FlushIntervalMs > 0 {
		tunables.FlushInterval = time.Duration(welcome.FlushIntervalMs) * time.Millisecond
	}
	if welcome.BatchMax > 0 {
		tunables.BatchMax = welcome.BatchMax
	}
	if welcome.BufferMax > 0 {
		tunables.BufferMax = welcome.BufferMax
	}
	return tunables
}

// Connect says hello unless the probe already has, and until the server answers it: a probe that
// is refused keeps trying, and never gives up. It returns only when hello is answered or ctx ends.
//
// Callers say Connect before every call, since a failed call loses the hello: whoever
// finds it lost says hello again, and the others wait for that one.
func (s *Session) Connect(ctx context.Context) error {
	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.gate }()

	if s.isGreeted() {
		return nil
	}
	var backoff Backoff
	for {
		callCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		welcome, err := s.client.Hello(callCtx, &s.hello)
		cancel()
		if err == nil {
			s.greet(welcome)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.wait(ctx, s.failed(callHello, err, &backoff)); err != nil {
			return err
		}
	}
}

func (s *Session) isGreeted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.greeted
}

// lose forgets the hello, so that the next Connect says it again
func (s *Session) lose() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.greeted = false
}

// greet takes the server's answer to hello, logs who the probe is connected as, and warns when the
// server's clock is not the probe's
func (s *Session) greet(welcome *probeproto.HelloResponse) {
	s.mu.Lock()
	s.welcome = welcome
	s.greeted = true
	s.mu.Unlock()

	s.logger.Infow("Connected to the server",
		"probe", welcome.Probe.Name,
		"location", welcome.Probe.Location,
		"server_version", welcome.ServerVersion,
		"protocol_version", welcome.ProtocolVersion)

	now := s.now()
	if skew := welcome.ServerTime.Sub(now); skew > maxClockSkew || skew < -maxClockSkew {
		s.logger.Warnw("The server's clock differs from this probe's by more than the limit; results are stamped by the probe's clock",
			"skew", skew.String(),
			"limit", maxClockSkew.String(),
			"server_time", welcome.ServerTime,
			"probe_time", now)
	}
}

// failed decides what a failed call means, logs it, and returns how long to wait before the next
// attempt. call names what failed. backoff is the caller's own: the wait it advances is the one for
// a server that doesn't answer, and the caller resets it when a call is answered.
//
//   - 409: the server doesn't know this instance. Say hello again at once, but only the first time
//     since a call was answered: a 409 after that hello means the server doesn't keep the
//     instance, so back off. Said to hello itself it can't mean that, so it is any other failure:
//     back off.
//   - 426: the server needs a newer protocol than the probe speaks. Log the server's message and
//     come back in an hour: an upgrade of the probe is what helps.
//   - 401: the token is refused, which an operator can put right on the server. Back off.
//   - 429 and 503: wait for the Retry-After the server states, up to five minutes.
//   - a network error or any other 5xx: back off, and say hello again when the server is back.
//   - any other status: back off.
//
// A network error or a 5xx also loses the hello, since the server may have restarted. After a 401,
// too: the instance was never recorded.
func (s *Session) failed(call string, err error, backoff *Backoff) time.Duration {
	var failure *probeproto.Error
	if !errors.As(err, &failure) {
		s.lose()
		wait := backoff.Next()
		s.logger.Warnw("Lost the server", "call", call, "error", err, "retry_in", wait.String())
		return wait
	}

	switch {
	case failure.Status == http.StatusConflict && call != callHello:
		s.lose()
		if backoff.conflicted {
			wait := backoff.Next()
			s.logger.Warnw("The server doesn't know this instance, though it was greeted since the last answer", "call", call, "message", failure.Message, "retry_in", wait.String())
			return wait
		}
		backoff.conflicted = true
		s.logger.Infow("The server doesn't know this instance; saying hello again", "call", call, "message", failure.Message)
		return 0
	case failure.Status == http.StatusUpgradeRequired:
		s.lose()
		s.logger.Errorw("The server doesn't speak this probe's protocol version; upgrade the probe. Trying again in an hour",
			"call", call, "message", failure.Message, "protocol_version", probeproto.ProtocolVersion, "retry_in", upgradeRetry.String())
		return upgradeRetry
	case failure.Status == http.StatusUnauthorized:
		s.lose()
		wait := backoff.Next()
		s.logger.Errorw("The server refused the probe token", "call", call, "message", failure.Message, "retry_in", wait.String())
		return wait
	case failure.Status == http.StatusTooManyRequests, failure.Status == http.StatusServiceUnavailable:
		if failure.Status == http.StatusServiceUnavailable {
			s.lose()
		}
		wait := min(failure.RetryAfter, maxRetryAfter)
		if wait <= 0 {
			wait = backoff.Next()
		}
		s.logger.Warnw("The server asked the probe to wait", "call", call, "status", failure.Status, "message", failure.Message, "retry_in", wait.String())
		return wait
	default:
		if failure.Status >= http.StatusInternalServerError {
			s.lose()
		}
		wait := backoff.Next()
		s.logger.Warnw("The server failed a call", "call", call, "status", failure.Status, "message", failure.Message, "retry_in", wait.String())
		return wait
	}
}

// poll long-polls for the probe's monitors, as probeproto's client does, bounded by the poll
// timeout the server stated plus some slack
func (s *Session) poll(ctx context.Context, version string) (*probeproto.AssignmentsResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, s.Tunables().PollTimeout+pollSlack)
	defer cancel()
	return s.client.Assignments(ctx, version)
}

// post sends a batch of results, as probeproto's client does, bounded by the request timeout
func (s *Session) post(ctx context.Context, req *probeproto.ResultsRequest) (*probeproto.ResultsResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return s.client.Results(ctx, req)
}

// Bye tells the server this instance is stopping, so that it deals the instance's monitors out at
// once. ctx bounds the call.
func (s *Session) Bye(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return s.client.Bye(ctx)
}
