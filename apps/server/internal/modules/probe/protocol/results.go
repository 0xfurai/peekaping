package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"runtime/debug"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/shared"
	"peekaping/internal/probeproto"
	"peekaping/internal/reporting"
	"peekaping/internal/utils"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
)

const (
	// maxResultAge is how long before the server's clock a check may have started and still be
	// stored. An older result is too_old.
	maxResultAge = time.Hour
	// maxResultSkew is how far ahead of the server's clock a check may have started: a probe
	// whose clock runs a little fast is not wrong. A result from further ahead is invalid.
	maxResultSkew = time.Minute
	// maxResultPingMs keeps the response time inside the INTEGER column of the heartbeats
	maxResultPingMs = math.MaxInt32
	// PendingRetryAfter is the Retry-After of a batch answered 503 because another request holds
	// the id of one of its results: the probe sends the batch again after that long. The holder is
	// usually storing, which takes seconds; if it died, its claim runs out after ClaimTTL, and the
	// probe asks again every PendingRetryAfter until then.
	PendingRetryAfter = 5 * time.Second
	// messagePending is the 503's message
	messagePending = "a result in this batch is being stored by another request: send the batch again"
	// messageStoreDeadline is the message of the 503 for a request that stored until its deadline
	messageStoreDeadline = "storing the results took too long: send the batch again"
)

// statusOfWord maps the status words of the wire to the statuses of heartbeats
var statusOfWord = map[string]shared.MonitorStatus{
	probeproto.StatusUp:          shared.MonitorStatusUp,
	probeproto.StatusDown:        shared.MonitorStatusDown,
	probeproto.StatusPending:     shared.MonitorStatusPending,
	probeproto.StatusMaintenance: shared.MonitorStatusMaintenance,
}

// candidate is a result that passed every check and waits to be stored. Its id is claimed in the
// accepted window from then on.
type candidate struct {
	result  *probeproto.Result
	monitor *monitor.Model
	status  shared.MonitorStatus
	tlsInfo *certificate.TLSInfo
	// stored is set once the ingester has taken the result
	stored bool
}

// Results takes the results of the checks that an instance ran, and answers with one outcome for
// each, in the order sent:
//   - invalid: the result is malformed, or its start is more than a minute ahead of the clock
//   - too_old: the check started more than an hour ago
//   - unknown_monitor: there is no such monitor
//   - not_assigned: the monitor is not one of the probe's that the instance may send results for (it
//     is paused, a push monitor, a type the instance's build can't run, or another probe's)
//   - duplicate: the probe sent this id before, and the server stored it
//
// The rest are stored with the monitor's settings and maintenance state, as the push URL stores
// a push, one monitor at a time and each monitor's results oldest first, because storing a result
// reads the monitor's previous heartbeat. The answer is sent after the last is stored. A result that fails to store is the server's failure and not the
// result's: the whole batch is answered 500, so the probe sends it again, and the results that
// were stored by then are answered duplicate.
//
// The ids of the rest are claimed in the accepted window before anything is stored. An id whose
// result was not stored when the request ends is given back. An id whose result was stored is kept
// for the hour as soon as its monitor's results are stored: a claim alone runs out in ClaimTTL, for
// the retry of a request that died to be accepted.
//
// A retry that arrives while another request is still storing finds those ids claimed and not kept
// (pending). The holder may be at it, or may have died, and the probe drops a duplicate for good, so
// the result is not answered duplicate. It is not stored either: the rest of the batch is stored as
// usual, and the whole request is answered 503 with a Retry-After of PendingRetryAfter. The probe
// keeps the batch and sends it again, and finds the results stored by then answered duplicate.
//
// A request stores for StoreDeadline at the most, which is shorter than ClaimTTL, so that it is over
// before its claims can run out. When the deadline hits, what was stored is kept, what was not is
// given back, and the answer is the same 503.
func (c *Controller) Results(ctx context.Context, input *ResultsInput) (*httpx.Output[probeproto.ResultsResponse], error) {
	// MARK: Results

	r := input.Request
	owner, instanceID, ok := ProbeFromContext(r.Context())
	if !ok {
		return nil, c.unauthenticated()
	}
	var req probeproto.ResultsRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if len(req.Results) > BatchMax {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(
			"batch holds "+strconv.Itoa(len(req.Results))+" results, the limit is "+strconv.Itoa(BatchMax)))
	}
	outcomes := make([]probeproto.ResultOutcome, len(req.Results))
	if len(req.Results) == 0 {
		return httpx.Reply(http.StatusOK, probeproto.ResultsResponse{Results: outcomes})
	}

	// The types this instance runs say which monitors it may send results for: a build that lacks
	// an executor can't have run the check
	instance, err := c.service.FindInstance(r.Context(), owner.ID, instanceID)
	if err != nil {
		return nil, c.fail(err)
	}
	if instance == nil {
		// Gone since the middleware stamped it (replaced to make room, or pruned): it has to say hello
		return nil, httpx.Fail(http.StatusConflict, utils.NewFailResponse(messageUnknownInstance))
	}
	owned, err := c.dealtMonitors(r.Context(), owner, instance)
	if err != nil {
		return nil, c.fail(err)
	}
	now := c.now().UTC()

	// others are the monitors that results name and the probe is not dealt, read once each; a nil
	// is a monitor that does not exist
	others := map[string]*monitor.Model{}
	var waiting []*candidate
	// claimedHere are the ids this request claimed, and heldElsewhere is whether the window found
	// an id of the batch pending: claimed by another request that has not stored its result
	claimedHere := map[string]bool{}
	heldElsewhere := false
	// What was claimed and not stored goes back to the window, whether the request fails or panics:
	// the probe sends such a result again, and it has to be accepted
	defer func() {
		for _, w := range waiting {
			if w.stored {
				continue
			}
			if err := c.accepted.Release(r.Context(), owner.ID, w.result.ID); err != nil {
				// The id stays claimed until the window forgets it, so the probe's retry of this
				// result is answered duplicate though it was not stored
				c.logger.Errorw("Failed to give back the id of a result that was not stored",
					"probeId", owner.ID, "resultId", w.result.ID, "error", err)
			}
		}
	}()
	for i := range req.Results {
		result := &req.Results[i]
		outcomes[i] = probeproto.ResultOutcome{ID: result.ID}

		status, tlsInfo, reason := judge(result, now)
		var m *monitor.Model
		if reason == "" {
			m = owned[result.MonitorID]
			if m == nil {
				other, err := c.otherMonitor(r.Context(), others, result.MonitorID)
				if err != nil {
					return nil, c.fail(err)
				}
				if dealtTo(other, owner, instance) {
					// The listing missed it: paging by offset skips a monitor when another is paused
					// or deleted during the scan. The probe drops a not_assigned result for good, so
					// the monitor's own read decides.
					m = other
				} else {
					reason = notAssignedOrUnknown(other)
				}
			}
		}
		// The claim is the last check, so that only a result that goes on to be stored takes an id.
		// An id twice in one batch is claimed by the first.
		if reason == "" && claimedHere[result.ID] {
			reason = probeproto.ReasonDuplicate
		}
		if reason == "" {
			claim, err := c.accepted.Claim(r.Context(), owner.ID, result.ID, now)
			if err != nil {
				// The window could not say whether the id was taken: the batch is the probe's to
				// send again, and the ids claimed so far are given back
				return nil, c.unavailable("claiming a result id", err)
			}
			switch claim {
			case ClaimWon:
				claimedHere[result.ID] = true
			case ClaimPending:
				// Another request holds the id and has not stored the result: it may be at it, or
				// it may have died. This one is neither stored nor a duplicate, which the probe
				// would drop for good. The rest of the batch goes on, and the probe sends the
				// whole batch again.
				heldElsewhere = true
				continue
			default:
				reason = probeproto.ReasonDuplicate
			}
		}
		if reason != "" {
			outcomes[i].Reason = reason
			continue
		}
		outcomes[i].Accepted = true
		waiting = append(waiting, &candidate{result: result, monitor: m, status: status, tlsInfo: tlsInfo})
	}

	// A request stores for StoreDeadline at the most, so that it is over before its claims run out
	storeCtx, cancelStore := context.WithTimeout(r.Context(), c.timing.StoreDeadline)
	defer cancelStore()
	if err := c.store(storeCtx, owner, waiting); err != nil {
		if storeCtx.Err() != nil && r.Context().Err() == nil {
			// The deadline, not the probe going away: what was stored is kept and the rest is given
			// back on the way out, and the probe sends the batch again
			c.logger.Errorw("Storing a batch of results took until the deadline",
				"probeId", owner.ID, "deadline", c.timing.StoreDeadline.String(), "error", err)
			return nil, c.retryLater(owner.ID, messageStoreDeadline)
		}
		return nil, c.fail(err)
	}
	if heldElsewhere {
		return nil, c.retryLater(owner.ID, messagePending)
	}
	return httpx.Reply(http.StatusOK, probeproto.ResultsResponse{Results: outcomes})
}

// retryLater answers 503 with a Retry-After for a batch that has results left to store: one whose
// id another request holds, or that was not stored before the deadline. The probe keeps the batch
// and sends it again after PendingRetryAfter: the results stored by now are duplicates then, and
// the rest are stored, or a duplicate if the holder stored them, or told to wait again if the
// holder is still at it or its claim has not run out yet.
func (c *Controller) retryLater(probeID, message string) error {
	c.logger.Infow("Asked a probe to send a batch again", "probeId", probeID, "why", message,
		"retryAfter", PendingRetryAfter.String())
	retryAfter := http.Header{"Retry-After": {strconv.Itoa(int(PendingRetryAfter / time.Second))}}
	return huma.ErrorWithHeaders(httpx.Fail(http.StatusServiceUnavailable, utils.NewFailResponse(message)), retryAfter)
}

// dealtMonitors are the monitors that the instance may send results for, by id: the active monitors
// its probe runs of the types the instance supports. They are the monitors it can be dealt and not
// only those it is dealt now: a monitor moves to another instance when one joins or leaves, and the
// results of a check the first had under way are still its to send.
func (c *Controller) dealtMonitors(ctx context.Context, owner *probe.Model, instance *probe.Instance) (map[string]*monitor.Model, error) {
	monitors, err := c.source.FindActiveByProbe(ctx, owner.ID)
	if err != nil {
		return nil, err
	}
	dealt := Dealable(monitors, instance.SupportedTypes)
	byID := make(map[string]*monitor.Model, len(dealt))
	for _, m := range dealt {
		byID[m.ID] = m
	}
	return byID, nil
}

// dealtTo reports whether the instance may send results for the monitor, by the rule that
// dealtMonitors lists by: the monitor is active, runs on the instance's probe, and is of a type the
// instance supports. m is nil when there is no such monitor.
func dealtTo(m *monitor.Model, owner *probe.Model, instance *probe.Instance) bool {
	return m != nil && m.RunsOn(owner.ID) && len(Dealable([]*monitor.Model{m}, instance.SupportedTypes)) == 1
}

// otherMonitor reads a monitor that the listing did not deal the probe, to tell one that does not
// exist from one that is not the probe's, or that the listing skipped. It reads each monitor once
// for a batch: others holds what it read.
// An id that is not a UUID names no monitor, and is not read: monitors.id is a UUID column, and
// PostgreSQL fails the lookup of anything else, which would fail the whole batch for the probe to
// send again for good.
func (c *Controller) otherMonitor(ctx context.Context, others map[string]*monitor.Model, id string) (*monitor.Model, error) {
	if m, read := others[id]; read {
		return m, nil
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, nil
	}
	m, err := c.monitors.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	others[id] = m
	return m, nil
}

// notAssignedOrUnknown is the reason a result is rejected for naming a monitor that the probe is
// not dealt: m is the monitor, nil when there is none
func notAssignedOrUnknown(m *monitor.Model) string {
	if m == nil {
		return probeproto.ReasonUnknownMonitor
	}
	return probeproto.ReasonNotAssigned
}

// judge checks a result by itself, without the monitor it names or the results before it. It
// returns the status and the TLS information the result's words stand for, or the reason to
// reject it.
func judge(r *probeproto.Result, now time.Time) (shared.MonitorStatus, *certificate.TLSInfo, string) {
	status, known := statusOfWord[r.Status]
	switch {
	case r.ID == "", r.MonitorID == "", !known:
		return 0, nil, probeproto.ReasonInvalid
	case r.StartTime.IsZero(), r.EndTime.Before(r.StartTime), r.StartTime.After(now.Add(maxResultSkew)):
		return 0, nil, probeproto.ReasonInvalid
	case r.PingMs < 0, r.PingMs > maxResultPingMs:
		return 0, nil, probeproto.ReasonInvalid
	}
	tlsInfo, err := decodeTLSInfo(r.TLSInfo)
	if err != nil {
		return 0, nil, probeproto.ReasonInvalid
	}
	if r.StartTime.Before(now.Add(-maxResultAge)) {
		return 0, nil, probeproto.ReasonTooOld
	}
	return status, tlsInfo, ""
}

// decodeTLSInfo reads the certificate information of a result: nil when it has none
func decodeTLSInfo(raw json.RawMessage) (*certificate.TLSInfo, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var info certificate.TLSInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// store hands the results to the ingester, those of different monitors at the same time, up to
// StoreConcurrency monitors, and each monitor's one at a time, oldest first, and marks each one
// stored and its id kept in the window. Ingest reads a monitor's previous heartbeat, so a monitor's
// results can't overlap; those of different monitors don't depend on one another, and storing a
// result is a few queries, which a batch of hundreds stored one after another would wait for in turn.
//
// A monitor whose result fails to store stops there, leaving that result and the ones after it not
// stored, and no monitor starts after that. The monitors being stored by then finish, so the results
// stored when the request ends are the ones its answer, a 500, leaves to be answered duplicate when
// the probe sends the batch again. The error returned is the first failed monitor's, in the order
// the monitors were sent.
func (c *Controller) store(ctx context.Context, owner *probe.Model, waiting []*candidate) error {
	var order []string
	byMonitor := map[string][]*candidate{}
	for _, w := range waiting {
		id := w.monitor.ID
		if _, seen := byMonitor[id]; !seen {
			order = append(order, id)
		}
		byMonitor[id] = append(byMonitor[id], w)
	}

	var (
		wg       sync.WaitGroup
		slots    = make(chan struct{}, StoreConcurrency)
		failed   atomic.Bool
		failures = make([]error, len(order))
	)
	for i, monitorID := range order {
		// A slot of this request, then one of the server's, which the requests of all the probe
		// instances share. A monitor that has its slots waits for nothing else but the database, and
		// gives them back, so a request waiting for a slot of the server's gets one.
		slots <- struct{}{}
		c.storeSlots <- struct{}{}
		if failed.Load() {
			<-c.storeSlots
			<-slots
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			defer func() { <-c.storeSlots }()
			defer func() {
				// A panic here is not on the request's goroutine, where the router would recover it
				if value := recover(); value != nil {
					reporting.CapturePanic("http", value, debug.Stack())
					failures[i] = fmt.Errorf("panic storing a result of monitor %s from probe %s: %v", monitorID, owner.ID, value)
					failed.Store(true)
				}
			}()
			if err := c.storeMonitor(ctx, owner, monitorID, byMonitor[monitorID]); err != nil {
				failures[i] = err
				failed.Store(true)
			}
		}()
	}
	wg.Wait()
	for _, err := range failures {
		if err != nil {
			return err
		}
	}
	return nil
}

// storeMonitor hands one monitor's results to the ingester, oldest first, and stops at the first
// that fails
func (c *Controller) storeMonitor(ctx context.Context, owner *probe.Model, monitorID string, group []*candidate) error {
	slices.SortStableFunc(group, func(a, b *candidate) int { return a.result.StartTime.Compare(b.result.StartTime) })

	// The ids of the results stored so far are kept when this returns, the failed or panicked
	// storing of a later result included: a claim runs out in minutes, and a result that was stored
	// must stay taken for the probe's retry. One call for the group, one pipeline in Redis.
	var stored []string
	defer func() { c.keepStored(ctx, owner.ID, stored) }()

	// The window the monitor is in is the same for each result of the batch
	underMaintenance, err := healthcheck.MonitorUnderMaintenance(ctx, c.maintenance, monitorID)
	if err != nil {
		// Keep the results the probe sent rather than lose them
		c.logger.Errorw("Failed to check if a monitor a probe sent results for is under maintenance",
			"probeId", owner.ID, "monitorId", monitorID, "error", err)
	}
	for _, w := range group {
		result := healthcheck.ResultFromMonitor(w.monitor, underMaintenance, w.status, w.result.Message, w.result.PingMs,
			w.result.StartTime.UTC(), w.result.EndTime.UTC(), w.tlsInfo, c.logger)
		result.ProbeID = owner.ID
		if err := c.ingester.Ingest(ctx, result); err != nil {
			return fmt.Errorf("failed to store a result of monitor %s from probe %s: %w", monitorID, owner.ID, err)
		}
		w.stored = true
		stored = append(stored, w.result.ID)
	}
	return nil
}

// keepStored has the window hold the ids of results that were stored for the rest of the hour. A
// window that fails to is the log's business, not the request's: the results are stored already,
// and failing the request would have the probe send them again. The ids run out of the window with
// their claims, in minutes, and a retry after that stores the results twice.
func (c *Controller) keepStored(ctx context.Context, probeID string, ids []string) {
	if len(ids) == 0 {
		return
	}
	if err := c.accepted.Keep(ctx, probeID, ids...); err != nil {
		c.logger.Errorw("Failed to keep the ids of results that were stored",
			"probeId", probeID, "resultIds", len(ids), "error", err)
	}
}
