package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"peekaping/internal/version"

	"go.uber.org/zap"
)

// Source is where the sender gets the ping from, and where it records having tried
type Source interface {
	// NextPing returns the ping to send at now, or nil when none is due: the admin opted out,
	// the environment forces the ping off, or the last ping was attempted less than a day ago.
	NextPing(ctx context.Context, now time.Time) (*Payload, error)
	// RecordSent records that the ping carrying instanceID was attempted at the given time. It
	// records nothing when instanceID is no longer the stored one: the admin opted out, or out
	// and in again, while the ping was on its way, and that ping is not the new opt-in's.
	RecordSent(ctx context.Context, instanceID string, at time.Time) error
}

// Schedule is when a Sender checks whether a ping is due
type Schedule struct {
	// FirstCheck is how long after Start the first check runs
	FirstCheck time.Duration
	// Every is the time between checks
	Every time.Duration
}

// DefaultSchedule checks a few minutes after the start, then hourly
var DefaultSchedule = Schedule{FirstCheck: 5 * time.Minute, Every: time.Hour}

// sendTimeout is how long a ping may take. A slower one is dropped.
const sendTimeout = 10 * time.Second

// recordTimeout is how long the sender has to record an attempt once it is stopped
const recordTimeout = 5 * time.Second

// Sender sends the usage ping once a day, in the background. Everything that goes wrong is logged
// at debug and dropped: the ping must never get in the way of the checks or the API.
type Sender struct {
	source   Source
	claimer  Claimer
	endpoint string
	logger   *zap.SugaredLogger
	client   *http.Client
	now      func() time.Time
}

// SenderOption changes how a Sender works; tests use them
type SenderOption func(*Sender)

// WithClock replaces the clock
func WithClock(now func() time.Time) SenderOption {
	return func(s *Sender) { s.now = now }
}

// WithTimeout replaces the time a ping may take
func WithTimeout(timeout time.Duration) SenderOption {
	return func(s *Sender) { s.client.Timeout = timeout }
}

func NewSender(source Source, claimer Claimer, endpoint string, logger *zap.SugaredLogger, opts ...SenderOption) *Sender {
	s := &Sender{
		source:   source,
		claimer:  claimer,
		endpoint: endpoint,
		logger:   logger.Named("[telemetry-sender]"),
		client: &http.Client{
			Timeout: sendTimeout,
			// The ping goes only to the endpoint it is configured for
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		now: time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Start checks on the schedule until ctx is done. The channel it returns is closed once the
// background checks have stopped, so that the database can close after them.
func (s *Sender) Start(ctx context.Context, schedule Schedule) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTimer(schedule.FirstCheck)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				s.SendIfDue(ctx)
				timer.Reset(schedule.Every)
			}
		}
	}()
	return done
}

// SendIfDue sends the ping when one is due and this process wins the claim. Nothing it does can
// fail the caller: a failure is logged at debug and the ping dropped, and a panic is recovered.
// The attempt is recorded whether or not the ping got through, so that a day is a day.
func (s *Sender) SendIfDue(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Errorw("Usage ping panicked, dropping it", "panic", fmt.Sprint(r))
		}
	}()

	payload, err := s.source.NextPing(ctx, s.now())
	if err != nil {
		s.logger.Debugw("Failed to check whether a usage ping is due, dropping the check", "error", err)
		return
	}
	if payload == nil {
		return
	}

	claimed, err := s.claimer.Claim(ctx)
	if err != nil {
		s.logger.Debugw("Failed to claim the day's usage ping, dropping the check", "error", err)
		return
	}
	if !claimed {
		s.logger.Debugw("Another process sends the day's usage ping")
		return
	}

	if err := s.send(ctx, payload); err != nil {
		s.logger.Debugw("Failed to send the usage ping, dropping it", "error", err)
	}

	// Even when the shutdown cut the ping short: it may have got through
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	if err := s.source.RecordSent(recordCtx, payload.InstanceID, s.now()); err != nil {
		s.logger.Debugw("Failed to record the usage ping", "error", err)
	}
}

// send posts the payload as JSON and discards the answer
func (s *Sender) send(ctx context.Context, payload *Payload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode the payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Peekaping/"+version.Version)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("post the payload: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("the endpoint answered %s", resp.Status)
	}
	return nil
}
