package protocol_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/probeproto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// releaseFailingWindow is a window whose Release fails, after it has been recorded
type releaseFailingWindow struct {
	*brokenWindow
}

func (w *releaseFailingWindow) Release(ctx context.Context, probeID, id string) error {
	_ = w.brokenWindow.Release(ctx, probeID, id)
	return errors.New("redis: connection refused")
}

func TestResults_AFailureToFindTheInstanceAnswers500AndStoresNothing(t *testing.T) {
	s := newResultsServer(t)
	s.probes.instanceErr = errors.New("the database is down")

	w := s.post(t, probeproto.ResultsRequest{Results: []probeproto.Result{checkResult("a", monCheckout, time.Now().UTC().Add(-time.Minute))}})

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Equal(t, "Internal server error", errorMessageOf(t, w))
	assert.Empty(t, s.ingest.attempts)
	assert.Zero(t, s.source.calls, "no monitor was read")
}

// Giving an id back is the log's business when the window can't: the answer is the one the failure
// that ended the request gave, and the other ids are still given back
func TestResults_AReleaseThatFailsDoesNotStopTheOthersOrChangeTheAnswer(t *testing.T) {
	window := &releaseFailingWindow{&brokenWindow{Window: protocol.NewMemoryWindow(), failFrom: 1000}}
	s := newResultsServerWith(t, window)
	start := time.Now().UTC().Add(-time.Minute)
	s.ingest.failOn = func(*ingester.Result) error { return errors.New("the database is down") }

	w := s.post(t, probeproto.ResultsRequest{Results: []probeproto.Result{
		checkResult("first", monCheckout, start), checkResult("second", monSearch, start),
	}})

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Equal(t, []string{"first", "second"}, window.released, "each id claimed and not stored is given back, in the order sent")
}

// A panic in the ingester is not on the request's goroutine, where the router would recover it
func TestResults_APanicWhileStoringFailsTheRequestAndFreesItsIds(t *testing.T) {
	s := newResultsServer(t)
	start := time.Now().UTC().Add(-time.Minute)
	s.ingest.hold = func(*ingester.Result) { panic("the ingester broke") }

	w := s.post(t, probeproto.ResultsRequest{Results: []probeproto.Result{checkResult("a", monSearch, start)}})

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Equal(t, "Internal server error", errorMessageOf(t, w))
	assert.Empty(t, s.ingest.stored)

	s.ingest.hold = nil
	got := s.results(t, checkResult("a", monSearch, start))
	assert.Equal(t, []probeproto.ResultOutcome{accepted("a")}, got, "the id was given back, and the slots too")
	assert.Len(t, s.ingest.stored, 1)
}

// Once a monitor's results have failed to store, no monitor starts after it
func TestResults_NoMonitorStartsStoringAfterAnotherFailed(t *testing.T) {
	s := newResultsServer(t)
	ids := s.addMonitors(protocol.StoreConcurrency + 5)
	start := time.Now().UTC().Add(-time.Minute)
	var batch []probeproto.Result
	for i, id := range ids {
		batch = append(batch, checkResult("r"+string(rune('a'+i)), id, start))
	}
	s.ingest.failOn = func(*ingester.Result) error { return errors.New("the database is down") }
	// The monitors that start before the first fails are all being stored before any fails, or a
	// fast failure would stop the others from starting
	watch := newInFlight(0)
	watch.waitForAll = protocol.StoreConcurrency
	s.ingest.hold = watch.hold

	w := s.post(t, probeproto.ResultsRequest{Results: batch})

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Len(t, s.ingest.attempts, protocol.StoreConcurrency, "the monitors being stored when the first failed were tried, and no other")

	s.ingest.failOn = nil
	s.ingest.hold = nil
	got := s.results(t, batch...)
	require.Len(t, got, len(batch))
	for _, outcome := range got {
		assert.True(t, outcome.Accepted, outcome.ID, "every id was given back")
	}
}
