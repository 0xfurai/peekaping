package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/shared"
	"peekaping/internal/probeproto"
	"peekaping/internal/utils"

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
	// outcome is the result's place in the answer, accepted until the store says otherwise
	outcome *probeproto.ResultOutcome
	// stored is set once the ingester has taken the result, or the queue held it already
	stored bool
}

// Results takes the results of the checks that an instance ran, and answers with one outcome for
// each, in the order sent:
//   - invalid: the result is malformed, or its start is more than a minute ahead of the clock
//   - too_old: the check started more than an hour ago
//   - unknown_monitor: there is no such monitor
//   - not_assigned: the monitor is not one of the probe's that it is dealt (it is paused, a push
//     monitor, a type the probe can't run, or another probe's)
//   - duplicate: the probe sent this id before, and the server stored it or is storing it
//
// The rest are stored with the monitor's settings and maintenance state, as the push URL stores
// a push, one monitor at a time and each monitor's results oldest first, because storing a result
// reads the monitor's previous heartbeat. The answer is sent after the last is stored or, in
// cluster mode, queued. A result that fails to store is the server's failure and not the
// result's: the whole batch is answered 500, so the probe sends it again, and the results that
// were stored by then are answered duplicate.
//
// The ids of the rest are claimed in the accepted window before anything is stored, so that a
// retry that arrives while this request is still storing is answered duplicate. An id whose result
// was not stored when the request ends is given back.
func (c *Controller) Results(ctx context.Context, input *ResultsInput) (*httpx.Output[probeproto.ResultsResponse], error) {
	// MARK: Results

	r := input.Request
	owner, _, ok := ProbeFromContext(r.Context())
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

	owned, err := c.dealtMonitors(r.Context(), owner)
	if err != nil {
		return nil, c.fail(err)
	}
	now := c.now().UTC()

	// others are the monitors that results name and the probe is not dealt, read once each; a nil
	// is a monitor that does not exist
	others := map[string]*monitor.Model{}
	var waiting []*candidate
	// What was claimed and not stored goes back to the window, whether the request fails or panics:
	// the probe sends such a result again, and it has to be accepted
	defer func() {
		for _, w := range waiting {
			if !w.stored {
				c.accepted.Release(owner.ID, w.result.ID)
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
				if dealtTo(other, owner) {
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
		if reason == "" && !c.accepted.Claim(owner.ID, result.ID, now) {
			reason = probeproto.ReasonDuplicate
		}
		if reason != "" {
			outcomes[i].Reason = reason
			continue
		}
		outcomes[i].Accepted = true
		waiting = append(waiting, &candidate{result: result, monitor: m, status: status, tlsInfo: tlsInfo, outcome: &outcomes[i]})
	}

	if err := c.store(r.Context(), owner, waiting); err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusOK, probeproto.ResultsResponse{Results: outcomes})
}

// dealtMonitors are the monitors that the probe is dealt, by id: the active monitors it runs of
// the types it supports. They are what it may send results for.
func (c *Controller) dealtMonitors(ctx context.Context, owner *probe.Model) (map[string]*monitor.Model, error) {
	monitors, err := c.source.FindActiveByProbe(ctx, owner.ID)
	if err != nil {
		return nil, err
	}
	dealt := Dealable(monitors, owner.SupportedTypes)
	byID := make(map[string]*monitor.Model, len(dealt))
	for _, m := range dealt {
		byID[m.ID] = m
	}
	return byID, nil
}

// dealtTo reports whether the probe is dealt the monitor, by the rule that dealtMonitors lists by:
// the monitor is active, runs on the probe, and is of a type the probe supports. m is nil when
// there is no such monitor.
func dealtTo(m *monitor.Model, owner *probe.Model) bool {
	return m != nil && m.RunsOn(owner.ID) && len(Dealable([]*monitor.Model{m}, owner.SupportedTypes)) == 1
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

// store hands the results to the ingester one monitor at a time, each monitor's oldest first, and
// marks each one stored, which keeps its id claimed. It stops at the first that fails, leaving that
// result and the ones after it not stored.
//
// In cluster mode the ingester enqueues the result, and the queue holds a task for the monitor and
// the start time for an hour. A task the queue holds already is a result stored, by a replica that
// the probe's first attempt reached: the result's outcome becomes duplicate, and it is not a failure.
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

	for _, monitorID := range order {
		group := byMonitor[monitorID]
		slices.SortStableFunc(group, func(a, b *candidate) int { return a.result.StartTime.Compare(b.result.StartTime) })

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
			err := c.ingester.Ingest(ctx, result)
			switch {
			case errors.Is(err, queue.ErrTaskIDConflict):
				w.outcome.Accepted, w.outcome.Reason = false, probeproto.ReasonDuplicate
			case err != nil:
				return fmt.Errorf("failed to store a result of monitor %s from probe %s: %w", monitorID, owner.ID, err)
			}
			w.stored = true
		}
	}
	return nil
}
