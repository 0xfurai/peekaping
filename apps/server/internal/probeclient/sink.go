package probeclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"go.uber.org/zap"

	"peekaping/internal/modules/ingester/ingest"
	"peekaping/internal/modules/shared"
	"peekaping/internal/probeproto"
)

const (
	// maxBatchBytes is how large, as JSON, a batch the probe sends may be. The server refuses a
	// request body over 1 MiB with 413 once it is decompressed, so the probe cuts a batch short
	// of that, with room for the envelope.
	maxBatchBytes = 900 << 10
	// dropLogEvery is how often, at most, results dropped from a full buffer are logged
	dropLogEvery = 10 * time.Second
)

// errNoOutcomes is the failure of a batch the server answered with no outcome for any of its results
var errNoOutcomes = errors.New("the server answered none of the results")

// Sink is where the probe's checks put their results: it implements ingest.Service. Ingest
// puts a result into a bounded buffer in memory, and never waits for the network. Run flushes the
// buffer to the server: a batch of at most batch_max results when the first result has waited
// flush_interval or batch_max have gathered, gzipped, oldest first. A result goes from the buffer
// when the server has answered it, accepted or not: a rejected result is dropped for good. One the
// server didn't answer stays, and is sent again with the ones after it, so a result sent after an
// outage arrives in order with the times of its check.
//
// The buffer holds buffer_max results. When it is full the oldest is dropped, counted and logged.
type Sink struct {
	session *Session
	logger  *zap.SugaredLogger
	ids     ulidGenerator

	// sendGate lets one batch on the wire at a time, the flusher's or a Flush's. It is a channel
	// so that a caller waiting for its turn can give up when its context ends.
	sendGate chan struct{}

	mu    sync.Mutex
	items []entry // oldest first
	// arrived is sent to when a result lands in an empty buffer
	arrived chan struct{}
	// endWait ends the flusher's wait for the flush interval; set only during that wait
	endWait context.CancelFunc
	// dropped counts the results lost to a full buffer, and unlogged those not yet logged
	dropped, unlogged int64
	lastDropLog       time.Time
}

var _ ingest.Service = (*Sink)(nil)

// entry is a buffered result and its size as JSON
type entry struct {
	result probeproto.Result
	size   int
}

// NewSink creates a sink that sends over the session. It sends nothing until Run or Flush does.
func NewSink(session *Session) *Sink {
	return &Sink{
		session:  session,
		logger:   session.loggerOf("probe_sink"),
		sendGate: make(chan struct{}, 1),
		arrived:  make(chan struct{}, 1),
	}
}

// Buffered is how many results wait to be sent
func (s *Sink) Buffered() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// Dropped is how many results a full buffer has lost, since the sink was made
func (s *Sink) Dropped() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

// Ingest puts the result in the buffer, with a ULID id made now. It returns an error only for a
// result the server could not read, which is not buffered.
func (s *Sink) Ingest(_ context.Context, r *ingest.Result) error {
	result, err := s.wireResult(r)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to encode the result of monitor %s: %w", r.MonitorID, err)
	}
	limits := s.session.Tunables()

	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.items) >= limits.BufferMax {
		s.items[0] = entry{}
		s.items = s.items[1:]
		s.drop(limits.BufferMax)
	}
	wasEmpty := len(s.items) == 0
	s.items = append(s.items, entry{result: result, size: len(raw) + 1})
	if wasEmpty {
		select {
		case s.arrived <- struct{}{}:
		default:
		}
	}
	if len(s.items) >= limits.BatchMax && s.endWait != nil {
		s.endWait()
	}
	return nil
}

// wireResult is the result as the server takes it
func (s *Sink) wireResult(r *ingest.Result) (probeproto.Result, error) {
	status, ok := statusWords[r.Status]
	if !ok {
		return probeproto.Result{}, fmt.Errorf("the result of monitor %s has status %d, which the probe protocol has no word for", r.MonitorID, r.Status)
	}
	result := probeproto.Result{
		ID:        s.ids.next(s.session.now()),
		MonitorID: r.MonitorID,
		Status:    status,
		Message:   r.Message,
		PingMs:    r.PingMs,
		StartTime: r.StartTime.UTC(),
		EndTime:   r.EndTime.UTC(),
	}
	if r.TLSInfo != nil {
		raw, err := json.Marshal(r.TLSInfo)
		if err != nil {
			return probeproto.Result{}, fmt.Errorf("failed to encode the certificate information of monitor %s: %w", r.MonitorID, err)
		}
		result.TLSInfo = raw
	}
	return result, nil
}

// statusWords are the status words of the wire, by status
var statusWords = map[shared.MonitorStatus]string{
	shared.MonitorStatusUp:          probeproto.StatusUp,
	shared.MonitorStatusDown:        probeproto.StatusDown,
	shared.MonitorStatusPending:     probeproto.StatusPending,
	shared.MonitorStatusMaintenance: probeproto.StatusMaintenance,
}

// drop counts the result dropped from the full buffer, and logs the drops now and then, since a
// probe that is cut off for long drops one result after another. The caller holds mu.
func (s *Sink) drop(bufferMax int) {
	s.dropped++
	s.unlogged++
	if now := s.session.now(); now.Sub(s.lastDropLog) >= dropLogEvery {
		s.logDrops(now, bufferMax)
	}
}

// logDrops logs the drops that have not been logged. The caller holds mu.
func (s *Sink) logDrops(now time.Time, bufferMax int) {
	s.logger.Warnw("The result buffer is full; dropped the oldest results",
		"dropped", s.unlogged, "total_dropped", s.dropped, "buffer_max", bufferMax)
	s.lastDropLog, s.unlogged = now, 0
}

// LogDrops logs the results dropped since the last drop log, if there are any. A drop is logged
// only once in a while, so those after the last log would be lost to the log if the probe stopped
// now.
func (s *Sink) LogDrops() {
	bufferMax := s.session.Tunables().BufferMax
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unlogged > 0 {
		s.logDrops(s.session.now(), bufferMax)
	}
}

// Run flushes the buffer until ctx ends: it waits until a batch is due, then sends the buffer. What
// is left when it returns stays in the buffer for Flush.
func (s *Sink) Run(ctx context.Context) {
	for s.awaitDue(ctx) {
		if s.drain(ctx) != nil {
			return
		}
	}
}

// awaitDue waits until there is a result and it is time to send: flush_interval after the first
// one, or at once when batch_max have gathered. It reports false when ctx ends first.
func (s *Sink) awaitDue(ctx context.Context) bool {
	for s.Buffered() == 0 {
		select {
		case <-s.arrived:
		case <-ctx.Done():
			return false
		}
	}

	limits := s.session.Tunables()
	waitCtx, endWait := context.WithCancel(ctx)
	defer endWait()
	s.mu.Lock()
	if len(s.items) >= limits.BatchMax {
		s.mu.Unlock()
		return true
	}
	s.endWait = endWait
	s.mu.Unlock()

	// A wait that a full batch cut short is as good as one that ran out
	_ = s.session.wait(waitCtx, limits.FlushInterval)

	s.mu.Lock()
	s.endWait = nil
	s.mu.Unlock()
	return ctx.Err() == nil
}

// Flush sends everything in the buffer, oldest first, without waiting for the interval. A call that
// fails is dealt with by the session's rules and tried again, so it returns when the buffer is empty
// or ctx ends: give it a deadline. Results sent by then are not lost if it returns early.
func (s *Sink) Flush(ctx context.Context) error {
	return s.drain(ctx)
}

// drain sends batches until the buffer is empty. It returns an error only when ctx ends.
func (s *Sink) drain(ctx context.Context) error {
	var backoff Backoff
	for s.Buffered() > 0 {
		if err := s.session.Connect(ctx); err != nil {
			return err
		}
		err := s.sendBatch(ctx)
		if err == nil {
			backoff.Reset()
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.session.wait(ctx, s.session.failed("send results", err, &backoff)); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// sendBatch sends the oldest results, as many as a batch holds, and takes from the buffer the ones
// the server answered
func (s *Sink) sendBatch(ctx context.Context) error {
	select {
	case s.sendGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.sendGate }()

	batch := s.oldest(s.session.Tunables().BatchMax)
	if len(batch) == 0 {
		return nil
	}
	results := make([]probeproto.Result, len(batch))
	for i, e := range batch {
		results[i] = e.result
	}
	answer, err := s.session.post(ctx, &probeproto.ResultsRequest{Results: results})
	if err != nil {
		return s.refusedBatch(batch, err)
	}
	if s.settle(batch, answer.Results) == 0 {
		return errNoOutcomes
	}
	return nil
}

// oldest copies the first results of the buffer: at most max, and not more than a batch may weigh
// (but always one)
func (s *Sink) oldest(max int) []entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var weight, n int
	for _, e := range s.items {
		if n >= max || (n > 0 && weight+e.size > maxBatchBytes) {
			break
		}
		weight += e.size
		n++
	}
	return slices.Clone(s.items[:n])
}

// refusedBatch deals with a batch the server refused. A single result too large for the server to
// read can never be accepted, so it is dropped rather than left to hold up every result behind it.
// Any other failure is returned for the session's rules to decide.
func (s *Sink) refusedBatch(batch []entry, err error) error {
	var failure *probeproto.Error
	if len(batch) == 1 && errors.As(err, &failure) && failure.Status == http.StatusRequestEntityTooLarge {
		s.logger.Errorw("The server refused a result as too large; dropped it",
			"monitor_id", batch[0].result.MonitorID, "size", batch[0].size, "message", failure.Message)
		s.remove(map[string]bool{batch[0].result.ID: true})
		return nil
	}
	return err
}

// settle takes the results the server answered out of the buffer, and returns how many that is.
// A rejected result is dropped like an accepted one.
func (s *Sink) settle(batch []entry, outcomes []probeproto.ResultOutcome) int {
	byID := make(map[string]probeproto.ResultOutcome, len(outcomes))
	for _, outcome := range outcomes {
		byID[outcome.ID] = outcome
	}
	answered := map[string]bool{}
	rejected := map[string]int{}
	var accepted, duplicates int
	for _, e := range batch {
		outcome, ok := byID[e.result.ID]
		switch {
		case !ok:
			continue
		case outcome.Accepted:
			accepted++
		case outcome.Reason == probeproto.ReasonDuplicate:
			duplicates++
		default:
			rejected[outcome.Reason]++
		}
		answered[e.result.ID] = true
	}
	s.remove(answered)

	if len(rejected) > 0 {
		s.logger.Warnw("The server rejected results; they are dropped", "rejected", rejected, "accepted", accepted)
	}
	s.logger.Debugw("Sent results", "sent", len(batch), "answered", len(answered), "accepted", accepted, "duplicates", duplicates)
	return len(answered)
}

// remove takes the results with these ids out of the buffer
func (s *Sink) remove(ids map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = slices.DeleteFunc(s.items, func(e entry) bool { return ids[e.result.ID] })
}
